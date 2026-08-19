package repository

import (
	"errors"
	"testing"

	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/mongo"
)

// TestMapDupKey duplicate key 错误映射为哨兵错误，其余原样返回。
func TestMapDupKey(t *testing.T) {
	sentinel := domain.ErrDuplicateFingerprint

	dupErr := mongo.CommandError{Code: 11000, Name: "DuplicateKey", Message: "E11000 duplicate key"}
	assert.ErrorIs(t, mapDupKey(dupErr, sentinel), sentinel)

	otherErr := errors.New("connection refused")
	assert.ErrorIs(t, mapDupKey(otherErr, sentinel), otherErr)
	assert.NoError(t, mapDupKey(nil, sentinel))
}

// TestObjectIDFromHex 非法 hex 返回 ErrInvalidID。
func TestObjectIDFromHex(t *testing.T) {
	_, err := objectIDFromHex("not-hex")
	assert.ErrorIs(t, err, domain.ErrInvalidID)

	id, err := objectIDFromHex("65f0c0f1f1f1f1f1f1f1f1f1")
	assert.NoError(t, err)
	assert.NotEqual(t, "000000000000000000000000", id.Hex())
}
