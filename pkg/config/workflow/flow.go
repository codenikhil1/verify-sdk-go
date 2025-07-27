package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"

	"github.com/ibm-verify/verify-sdk-go/internal/openapi"
	contextx "github.com/ibm-verify/verify-sdk-go/pkg/core/context"
	errorsx "github.com/ibm-verify/verify-sdk-go/pkg/core/errors"
)

type ModelTransformClient struct {
	Client *http.Client
}

type TransformModelParams = openapi.TransformSourceModelToTargetModelParams
type TransformModelResponse = openapi.TransformSourceModelToTargetModelObject

type ModelTransformRequest struct {
	ModelFile    io.Reader `json:"-"`
	TargetFormat string    `json:"targetFormat" yaml:"targetFormat"`
}

func NewModelTransformClient() *ModelTransformClient {
	return &ModelTransformClient{}
}

func (c *ModelTransformClient) TransformModel(ctx context.Context, modelFile io.Reader, targetFormat string) ([]byte, error) {
	vc := contextx.GetVerifyContext(ctx)
	client := openapi.NewClientWithOptions(ctx, vc.Tenant, c.Client)
	defaultErr := errorsx.G11NError("unable to transform model")

	// Create multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fmt.Printf("=== FORM CREATION DEBUG ===\n")
	fmt.Printf("Target format parameter: '%s'\n", targetFormat)

	// FIRST: Add the model file
	part, err := writer.CreateFormFile("model", "model.file")
	if err != nil {
		vc.Logger.Errorf("Unable to create form file; err=%v", err)
		return nil, defaultErr
	}

	fmt.Printf("=== FILE CONTENT DEBUG ===\n")
	// Read the file content to check what we're sending
	if seeker, ok := modelFile.(io.Seeker); ok {
		// If it's seekable, read a preview and reset
		previewBytes := make([]byte, 200)
		n, _ := modelFile.Read(previewBytes)
		fmt.Printf("File preview (first %d bytes): %s\n", n, string(previewBytes[:n]))

		// Reset to beginning
		seeker.Seek(0, 0)
	}

	// ONLY COPY ONCE
	bytesWritten, err := io.Copy(part, modelFile)
	if err != nil {
		vc.Logger.Errorf("Unable to copy model file; err=%v", err)
		return nil, defaultErr
	}
	fmt.Printf("✓ Added model file: %d bytes\n", bytesWritten)
	fmt.Printf("=== END FILE DEBUG ===\n")

	// SECOND: Add the targetformat field
	err = writer.WriteField("targetformat", targetFormat)
	if err != nil {
		vc.Logger.Errorf("Unable to write targetformat field; err=%v", err)
		return nil, defaultErr
	}
	fmt.Printf("✓ Added targetformat field with value: '%s'\n", targetFormat)

	// THIRD: Close the writer ONLY ONCE
	fmt.Printf("=== FORM FIELDS SUMMARY ===\n")
	fmt.Printf("1. model (file): %d bytes\n", bytesWritten)
	fmt.Printf("2. targetformat: %s\n", targetFormat)
	fmt.Printf("=== END SUMMARY ===\n")

	err = writer.Close()
	if err != nil {
		vc.Logger.Errorf("Unable to close multipart writer; err=%v", err)
		return nil, defaultErr
	}

	// Set up parameters
	params := &TransformModelParams{
		Authorization: "Bearer " + vc.Token,
	}

	// ADD THE REQUEST EDITORS (this was missing)
	reqEditors := []openapi.RequestEditorFn{
		func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Content-Type", writer.FormDataContentType())

			fmt.Printf("=== ACTUAL HTTP REQUEST ===\n")
			fmt.Printf("Method: %s\n", req.Method)
			fmt.Printf("URL: %s\n", req.URL.String())
			fmt.Printf("Headers:\n")
			for key, values := range req.Header {
				for _, value := range values {
					fmt.Printf("  %s: %s\n", key, value)
				}
			}
			fmt.Printf("=== END REQUEST ===\n")

			return nil
		},
	}

	// Make the API call
	resp, err := client.TransformSourceModelToTargetModelWithResponse(ctx, params, reqEditors...)
	if err != nil {
		vc.Logger.Errorf("Unable to transform model; err=%v", err)
		return nil, defaultErr
	}

	// Check response status
	if resp.StatusCode() != http.StatusOK {
		if err := errorsx.HandleCommonErrors(ctx, resp.HTTPResponse, "unable to transform model"); err != nil {
			vc.Logger.Errorf("unable to transform the model; err=%s", err.Error())
			return nil, errorsx.G11NError("unable to transform the model; err=%s", err.Error())
		}

		vc.Logger.Errorf("unable to transform the model; code=%d, body=%s", resp.StatusCode(), string(resp.Body))
		return nil, errorsx.G11NError("unable to transform the model; code=%d, body=%s", resp.StatusCode(), string(resp.Body))
	}

	return resp.Body, nil
}

func (c *ModelTransformClient) TransformModelFromFile(ctx context.Context, filePath, targetFormat string) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, errorsx.G11NError("unable to open model file; err=%v", err)
	}
	defer file.Close()

	return c.TransformModel(ctx, file, targetFormat)
}

func (c *ModelTransformClient) TransformModelFromRequest(ctx context.Context, req *ModelTransformRequest) ([]byte, error) {
	return c.TransformModel(ctx, req.ModelFile, req.TargetFormat)
}
