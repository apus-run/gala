// Command filehttp 是 components/fileserver 的生产示例：一个带元数据、秒传去重与
// 断点续传的文件托管服务（网盘后端）。
//
// 组装（对照传统 Gin+GORM 网盘的分层）：
//
//	api（HTTP 适配，ginx.W/B）→ service（业务，errorsx）→ repo（持久化，db+sqlite）
//	                                                    ↘ fileserver.Storage（字节层）
//
// 关键决策：
//   - 对象存储只当字节层：内容寻址、引用计数、用户关联、会话全在关系库。
//     元数据不依赖后端能力（filestore 不持久化 ContentType/Metadata 也不影响）。
//   - 上传写暂存对象的同时流式计算 SHA-256；登记成功后暂存 Key 即正式 Key，零拷贝。
//   - 断点续传不依赖 Redis：会话与分块位图在库里，分块是普通对象，合并是流式串联。
//   - 崩溃残留靠登记表回收（staging_objects / 过期会话），不需要存储端 List。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/apus-run/gala/components/db"
	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/api"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/config"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/model"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/repo"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/service"
	"github.com/apus-run/gala/components/fileserver/examples/filehttp/internal/ui"
	"github.com/apus-run/gala/components/fileserver/filestore"
	s3store "github.com/apus-run/gala/components/fileserver/objectstore/s3"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	maxUpload := cfg.Server.MaxUploadBytes

	storage, fs, closeStorage, err := newStorage(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}

	// sqlite 纯 Go 驱动，静态二进制可直接跑在 scratch/alpine 镜像里；
	// 换 MySQL/Postgres 只需替换这里的 Dialector。
	gormLogger := logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{
		SlowThreshold:             time.Second,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  false,
	})
	provider, err := db.NewDB(sqlite.Open(cfg.Database.DSN),
		&gorm.Config{TranslateError: true, Logger: gormLogger})
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	if err := provider.DB(ctx).AutoMigrate(
		&model.FileMeta{}, &model.UserFile{}, &model.UserFolder{}, &model.UploadSession{}, &model.StagingObject{},
	); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	r := repo.New(provider)
	files := service.NewFileService(r, storage, maxUpload)
	chunks := service.NewChunkService(files, r)

	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	api.NewFileHandler(files, chunks, maxUpload).Register(engine)
	ui.NewHandler(files, maxUpload).Register(engine)

	httpSrv := &http.Server{
		Addr:              cfg.Server.ListenAddr,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		// 不设 WriteTimeout：大文件下载会超过任何固定值；生命周期由 SIGTERM 优雅退出控制。
	}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()
	log.Printf("filehttp listening on %s (max upload %d bytes)", httpSrv.Addr, maxUpload)

	// 每小时一轮：filestore 半成品临时文件 + 过期上传会话与孤儿暂存对象。
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			if fs != nil {
				if n, err := fs.CleanupTemp(ctx, 24*time.Hour); err != nil {
					log.Printf("cleanup temp: %v", err)
				} else if n > 0 {
					log.Printf("cleanup temp: removed %d stale files", n)
				}
			}
			chunks.GC(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 先停流量，再关存储：等待进行中的调用退出。
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if err := closeStorage(); err != nil {
		log.Printf("close storage: %v", err)
	}
	if err := provider.Close(); err != nil {
		log.Printf("close database: %v", err)
	}
}

// newStorage 按环境选择字节层：S3_BUCKET 非空时用 objectstore/s3，否则用本地 filestore。
// S3_ENDPOINT 可指向 MinIO/RustFS 等兼容服务；AWS 原生时留空。
func newStorage(ctx context.Context, cfg config.Config) (fileserver.Storage, *filestore.Store, func() error, error) {
	if bucket := cfg.Storage.S3Bucket; bucket != "" {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Storage.S3Region))
		if err != nil {
			return nil, nil, nil, err
		}
		client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
			if ep := cfg.Storage.S3Endpoint; ep != "" {
				o.BaseEndpoint = aws.String(ep)
			}
			o.UsePathStyle = cfg.Storage.S3PathStyle
		})
		if cfg.Storage.S3CreateBucket {
			if _, err := client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(bucket)}); err != nil {
				var apiErr interface{ ErrorCode() string }
				if !errors.As(err, &apiErr) || (apiErr.ErrorCode() != "BucketAlreadyOwnedByYou" && apiErr.ErrorCode() != "BucketAlreadyExists") {
					return nil, nil, nil, fmt.Errorf("create s3 bucket %q: %w", bucket, err)
				}
			}
		}
		store, err := s3store.New(client, s3store.Config{
			Bucket:        bucket,
			Prefix:        cfg.Storage.S3Prefix,
			Concurrency:   4,
			MaxObjectSize: cfg.Server.MaxUploadBytes,
		})
		if err != nil {
			return nil, nil, nil, err
		}
		return store, nil, func() error { return nil }, nil
	}

	root := cfg.Storage.FSRoot
	// filestore 有意不自动建目录（防路径写错时悄悄建出新目录）；部署时由服务自建。
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, nil, nil, err
	}
	fs, err := filestore.New(root, filestore.WithMaxObjectSize(cfg.Server.MaxUploadBytes))
	if err != nil {
		return nil, nil, nil, err
	}
	return fs, fs, fs.Close, nil
}
