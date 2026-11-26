package mongo

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestHasUniqueNameIndex(t *testing.T) {
	tests := []struct {
		name  string
		specs []bson.M
		want  bool
	}{
		{
			name:  "unique name ascending",
			specs: []bson.M{{"key": bson.M{"name": int32(1)}, "unique": true}},
			want:  true,
		},
		{
			name:  "unique name ascending int64",
			specs: []bson.M{{"key": bson.M{"name": int64(1)}, "unique": true}},
			want:  true,
		},
		{
			name:  "non unique name",
			specs: []bson.M{{"key": bson.M{"name": int32(1)}}},
			want:  false,
		},
		{
			name:  "unique false",
			specs: []bson.M{{"key": bson.M{"name": int32(1)}, "unique": false}},
			want:  false,
		},
		{
			name: "compound key",
			specs: []bson.M{{
				"key":    bson.M{"name": int32(1), "ownerID": int32(1)},
				"unique": true,
			}},
			want: false,
		},
		{
			name:  "name descending",
			specs: []bson.M{{"key": bson.M{"name": int32(-1)}, "unique": true}},
			want:  false,
		},
		{
			name:  "other key",
			specs: []bson.M{{"key": bson.M{"token": int32(1)}, "unique": true}},
			want:  false,
		},
		{
			name: "partial unique index",
			specs: []bson.M{{
				"key":                     bson.M{"name": int32(1)},
				"unique":                  true,
				"partialFilterExpression": bson.M{"active": true},
			}},
			want: false,
		},
		{
			name: "sparse unique index is still exclusive",
			specs: []bson.M{{
				"key":    bson.M{"name": int32(1)},
				"unique": true,
				"sparse": true,
			}},
			want: true,
		},
		{
			name:  "empty",
			specs: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasUniqueNameIndex(tt.specs); got != tt.want {
				t.Fatalf("hasUniqueNameIndex() = %v, want %v", got, tt.want)
			}
		})
	}
}
