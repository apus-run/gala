package mongo

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	drivermongo "go.mongodb.org/mongo-driver/mongo"
)

func rawIndexKeys(t *testing.T, keys bson.D) bson.Raw {
	t.Helper()

	raw, err := bson.Marshal(keys)
	if err != nil {
		t.Fatalf("bson.Marshal(%v) error = %v", keys, err)
	}
	return bson.Raw(raw)
}

func TestHasUniqueNameIndex(t *testing.T) {
	yes, no := true, false
	nameAsc := rawIndexKeys(t, bson.D{{Key: "name", Value: int32(1)}})

	tests := []struct {
		name  string
		specs []*drivermongo.IndexSpecification
		want  bool
	}{
		{
			name:  "unique name ascending",
			specs: []*drivermongo.IndexSpecification{{KeysDocument: nameAsc, Unique: &yes}},
			want:  true,
		},
		{
			name:  "non unique name",
			specs: []*drivermongo.IndexSpecification{{KeysDocument: nameAsc, Unique: &no}},
			want:  false,
		},
		{
			name:  "nil unique flag",
			specs: []*drivermongo.IndexSpecification{{KeysDocument: nameAsc}},
			want:  false,
		},
		{
			name: "compound key",
			specs: []*drivermongo.IndexSpecification{{
				KeysDocument: rawIndexKeys(t, bson.D{
					{Key: "name", Value: int32(1)},
					{Key: "owner_id", Value: int32(1)},
				}),
				Unique: &yes,
			}},
			want: false,
		},
		{
			name: "name descending",
			specs: []*drivermongo.IndexSpecification{{
				KeysDocument: rawIndexKeys(t, bson.D{{Key: "name", Value: int32(-1)}}),
				Unique:       &yes,
			}},
			want: false,
		},
		{
			name: "other key",
			specs: []*drivermongo.IndexSpecification{{
				KeysDocument: rawIndexKeys(t, bson.D{{Key: "token", Value: int32(1)}}),
				Unique:       &yes,
			}},
			want: false,
		},
		{
			name:  "nil specification",
			specs: []*drivermongo.IndexSpecification{nil},
			want:  false,
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
