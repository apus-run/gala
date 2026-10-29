package repo

import (
	"reflect"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestOrWhere(t *testing.T) {
	tests := []struct {
		name  string
		build func() *MongoQuery[struct{}]
		want  bson.M
	}{
		{
			name: "empty filter",
			build: func() *MongoQuery[struct{}] {
				q := &MongoQuery[struct{}]{}
				q.OrWhere("a", 1)
				return q
			},
			want: bson.M{"$or": []bson.M{{"a": 1}}},
		},
		{
			name: "existing condition becomes first branch",
			build: func() *MongoQuery[struct{}] {
				q := &MongoQuery[struct{}]{filter: bson.M{"status": "active"}}
				q.OrWhere("status", "pending")
				return q
			},
			want: bson.M{"$or": []bson.M{{"status": "active"}, {"status": "pending"}}},
		},
		{
			name: "repeated OrWhere appends branches",
			build: func() *MongoQuery[struct{}] {
				q := &MongoQuery[struct{}]{filter: bson.M{"a": 1}}
				q.OrWhere("b", 2)
				q.OrWhere("c", 3)
				return q
			},
			want: bson.M{"$or": []bson.M{{"a": 1}, {"b": 2}, {"c": 3}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.build().filter; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("filter = %v, want %v", got, tt.want)
			}
		})
	}
}
