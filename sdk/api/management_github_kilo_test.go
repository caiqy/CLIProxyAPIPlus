package api

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func TestManagementTokenRequesterExposesGitHubMethod(t *testing.T) {
	t.Parallel()

	var requester any = (*managementTokenRequester)(nil)
	if _, ok := requester.(interface{ RequestGitHubToken(*gin.Context) }); !ok {
		t.Fatalf("managementTokenRequester must implement RequestGitHubToken")
	}
}

func TestManagementTokenRequesterExposesKiloMethod(t *testing.T) {
	t.Parallel()

	var requester any = (*managementTokenRequester)(nil)
	if _, ok := requester.(interface{ RequestKiloToken(*gin.Context) }); !ok {
		t.Fatalf("managementTokenRequester must implement RequestKiloToken")
	}
}
