package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagementTokenRequesterExposesKiroMethod(t *testing.T) {
	t.Parallel()

	var requester any = (*managementTokenRequester)(nil)
	if _, ok := requester.(interface{ RequestKiroToken(*gin.Context) }); !ok {
		t.Fatalf("managementTokenRequester must implement RequestKiroToken")
	}
}
