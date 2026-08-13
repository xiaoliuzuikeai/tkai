package image

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/service/image"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// [第一阶段优化-校验/HTTP] 图片接口增加大小校验并规范错误状态码。
type (
	RecognizeImageResponse struct {
		ClassName string `json:"class_name,omitempty"` // AI回答
		controller.Response
	}
)

func RecognizeImage(c *gin.Context) {
	res := new(RecognizeImageResponse)
	file, err := c.FormFile("image")
	if err != nil {
		log.Println("FormFile fail ", err)
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}
	// [第一阶段优化-校验] 图片限制为 1 字节到 10 MiB。
	if file.Size <= 0 || file.Size > 10<<20 {
		c.JSON(controller.HTTPStatus(code.CodeInvalidParams), res.CodeOf(code.CodeInvalidParams))
		return
	}

	className, err := image.RecognizeImage(file)
	if err != nil {
		log.Println("RecognizeImage fail ", err)
		c.JSON(controller.HTTPStatus(code.CodeServerBusy), res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.ClassName = className
	c.JSON(http.StatusOK, res)
}
