//go:build windows || !cgo

package image

import (
	"errors"
	"image"
)

type ImageRecognizer struct{}

func NewImageRecognizer(modelPath, labelPath string, inputH, inputW int) (*ImageRecognizer, error) {
	return nil, errors.New("image recognition requires ONNX Runtime with CGO and is not available in this build")
}

func (r *ImageRecognizer) Close() {}

func (r *ImageRecognizer) PredictFromFile(imagePath string) (string, error) {
	return "", errors.New("image recognition requires ONNX Runtime with CGO and is not available in this build")
}

func (r *ImageRecognizer) PredictFromBuffer(buf []byte) (string, error) {
	return "", errors.New("image recognition requires ONNX Runtime with CGO and is not available in this build")
}

func (r *ImageRecognizer) PredictFromImage(img image.Image) (string, error) {
	return "", errors.New("image recognition requires ONNX Runtime with CGO and is not available in this build")
}
