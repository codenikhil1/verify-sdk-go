package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

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

	bytesWritten, err := io.Copy(part, modelFile)
	if err != nil {
		vc.Logger.Errorf("Unable to copy model file; err=%v", err)
		return nil, defaultErr
	}
	fmt.Printf("✓ Added model file: %d bytes\n", bytesWritten)

	// SECOND: Add the targetformat field
	err = writer.WriteField("targetformat", targetFormat)
	if err != nil {
		vc.Logger.Errorf("Unable to write targetformat field; err=%v", err)
		return nil, defaultErr
	}
	fmt.Printf("✓ Added targetformat field with value: '%s'\n", targetFormat)

	// THIRD: Close the writer
	fmt.Printf("=== FORM FIELDS SUMMARY ===\n")
	fmt.Printf("1. model (file): %d bytes\n", bytesWritten)
	fmt.Printf("2. targetformat: %s\n", targetFormat)
	fmt.Printf("=== END SUMMARY ===\n")

	err = writer.Close()
	if err != nil {
		vc.Logger.Errorf("Unable to close multipart writer; err=%v", err)
		return nil, defaultErr
	}

	// Add the model file
	part, err1 := writer.CreateFormFile("model", "model.file")
	if err1 != nil {
		vc.Logger.Errorf("Unable to create form file; err=%v", err)
		return nil, defaultErr
	}

	_, err = io.Copy(part, modelFile)
	if err != nil {
		vc.Logger.Errorf("Unable to copy model file; err=%v", err)
		return nil, defaultErr
	}

	// Add targetformat parameter
	err = writer.WriteField("targetformat", targetFormat)
	if err != nil {
		vc.Logger.Errorf("Unable to write targetformat field; err=%v", err)
		return nil, defaultErr
	}

	// Close the writer to finalize the multipart form
	err = writer.Close()
	if err != nil {
		vc.Logger.Errorf("Unable to close multipart writer; err=%v", err)
		return nil, defaultErr
	}

	// Set up parameters
	params := &TransformModelParams{
		Authorization: "Bearer " + vc.Token,
	}

	// Create request editors for content type
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

			// ADD THIS: Log the actual form data being sent
			if req.Body != nil {
				bodyBytes, err := io.ReadAll(req.Body)
				if err == nil {
					fmt.Printf("Body Length: %d bytes\n", len(bodyBytes))
					bodyStr := string(bodyBytes)

					// Parse and show form fields clearly
					fmt.Printf("=== FORM DATA ANALYSIS ===\n")
					if strings.Contains(bodyStr, "Content-Disposition: form-data; name=\"model\"") {
						fmt.Printf("✓ Found 'model' field\n")
					} else {
						fmt.Printf("✗ Missing 'model' field\n")
					}

					if strings.Contains(bodyStr, "Content-Disposition: form-data; name=\"targetformat\"") {
						fmt.Printf("✓ Found 'targetformat' field\n")
						// Extract the value
						if idx := strings.Index(bodyStr, "name=\"targetformat\""); idx != -1 {
							substr := bodyStr[idx:]
							if end := strings.Index(substr, "\r\n\r\n"); end != -1 {
								if valueEnd := strings.Index(substr[end+4:], "\r\n"); valueEnd != -1 {
									value := substr[end+4 : end+4+valueEnd]
									fmt.Printf("  targetformat value: '%s'\n", value)
								}
							}
						}
					} else {
						fmt.Printf("✗ Missing 'targetformat' field\n")
					}

					// Check for other possible field names
					if strings.Contains(bodyStr, "name=\"targetFormat\"") {
						fmt.Printf("! Found 'targetFormat' (camelCase) field instead\n")
					}
					if strings.Contains(bodyStr, "name=\"target-format\"") {
						fmt.Printf("! Found 'target-format' (kebab-case) field instead\n")
					}

					// Show first 800 characters of the body for manual inspection
					fmt.Printf("=== RAW FORM DATA (first 800 chars) ===\n")
					if len(bodyStr) > 800 {
						fmt.Printf("%s...[truncated]\n", bodyStr[:800])
					} else {
						fmt.Printf("%s\n", bodyStr)
					}
					fmt.Printf("=== END FORM DATA ===\n")

					// Restore the body for the actual request
					req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
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
