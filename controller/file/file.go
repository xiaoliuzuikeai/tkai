package file

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/model"
	"GoAI/service/file"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// [第一阶段优化-校验/HTTP] 文件接口统一鉴权错误和参数错误的 HTTP 状态。
type (
	UploadFileResponse struct {
		FilePath string             `json:"file_path,omitempty"`
		Document *model.RAGDocument `json:"document,omitempty"`
		controller.Response
	}
	DocumentListResponse struct {
		Documents []model.RAGDocument `json:"documents"`
		controller.Response
	}
)

func UploadRagFile(c *gin.Context) {
	res := new(UploadFileResponse)
	uploadedFile, err := c.FormFile("file")
	if err != nil {
		log.Println("FormFile fail ", err)
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}

	username := c.GetString("userName")
	if username == "" {
		log.Println("Username not found in context")
		c.JSON(controller.HTTPStatus(code.CodeInvalidToken), res.CodeOf(code.CodeInvalidToken))
		return
	}

	//indexer 会在 service 层根据实际文件名创建
	document, err := file.UploadRagFile(c.Request.Context(), username, uploadedFile)
	if err != nil {
		log.Println("UploadFile fail ", err)
		c.JSON(controller.HTTPStatus(code.CodeServerBusy), res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.FilePath = document.ID
	res.Document = document
	c.JSON(http.StatusOK, res)
}

func ListRagDocuments(c *gin.Context) {
	res := new(DocumentListResponse)
	documents, err := file.ListRagDocuments(c.GetString("userName"))
	if err != nil {
		c.JSON(controller.HTTPStatus(code.CodeServerBusy), res.CodeOf(code.CodeServerBusy))
		return
	}
	res.Success()
	res.Documents = documents
	c.JSON(http.StatusOK, res)
}

func DeleteRagDocument(c *gin.Context) {
	res := new(controller.Response)
	if err := file.DeleteRagDocument(c.Request.Context(), c.GetString("userName"), c.Param("id")); err != nil {
		c.JSON(controller.HTTPStatus(code.CodeRecordNotFound), res.CodeOf(code.CodeRecordNotFound))
		return
	}
	res.Success()
	c.JSON(http.StatusOK, res)
}

func RetryRagDocument(c *gin.Context) {
	res := new(UploadFileResponse)
	document, err := file.RetryRagDocument(c.Request.Context(), c.GetString("userName"), c.Param("id"))
	if err != nil {
		c.JSON(controller.HTTPStatus(code.CodeServerBusy), res.CodeOf(code.CodeServerBusy))
		return
	}
	res.Success()
	res.Document = document
	c.JSON(http.StatusOK, res)
}
