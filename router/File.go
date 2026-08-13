package router

import (
	"GoAI/controller/file"

	"github.com/gin-gonic/gin"
)

func FileRouter(r *gin.RouterGroup) {
	r.POST("/upload", file.UploadRagFile)
	r.GET("/documents", file.ListRagDocuments)
	r.DELETE("/documents/:id", file.DeleteRagDocument)
	r.POST("/documents/:id/retry", file.RetryRagDocument)
}
