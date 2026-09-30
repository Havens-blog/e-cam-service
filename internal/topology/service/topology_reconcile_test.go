package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActivateResolvablePending_CallsRepoWithTenant(t *testing.T) {
	mock := newMockEdgeRepo()
	svc := &topologyService{edgeRepo: mock}

	svc.activateResolvablePending(context.Background(), 42)

	assert.Equal(t, []int64{42}, mock.activateResolvableCalls)
}