package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
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
	FileName     string    `json:"fileName" yaml:"fileName"`
}

func NewModelTransformClient() *ModelTransformClient {
	return &ModelTransformClient{}
}

func (c *ModelTransformClient) TransformModel(ctx context.Context, modelFile io.Reader, targetFormat string, filename string) ([]byte, error) {
	vc := contextx.GetVerifyContext(ctx)
	client := openapi.NewClientWithOptions(ctx, vc.Tenant, c.Client)
	defaultErr := errorsx.G11NError("unable to transform model")

	// Create multipart form data
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	fmt.Printf("=== FORM CREATION DEBUG ===\n")
	fmt.Printf("Target format parameter: '%s'\n", targetFormat)
	fmt.Printf("File Name parameter: '%s'\n", filename)
	// FIRST: Add the model file
	part, err := writer.CreateFormFile("model", filename)
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
	// Add this verification logging in your reqEditors:
	reqEditors := []openapi.RequestEditorFn{
		func(ctx context.Context, req *http.Request) error {
			// Set the multipart form data as the request body
			req.Body = io.NopCloser(&buf)
			req.ContentLength = int64(buf.Len())
			req.Header.Set("Content-Type", writer.FormDataContentType())

			fmt.Printf("=== ACTUAL HTTP REQUEST ===\n")
			fmt.Printf("Method: %s\n", req.Method)
			fmt.Printf("URL: %s\n", req.URL.String())
			fmt.Printf("Content-Length: %d\n", req.ContentLength)

			// **VERIFY EXACT FIELD COUNT**
			bodyContent := buf.String()

			// Count Content-Disposition headers (each form field has one)
			fieldCount := strings.Count(bodyContent, "Content-Disposition: form-data")
			fmt.Printf("Total form fields: %d\n", fieldCount)

			// Verify specific fields exist
			hasModelField := strings.Contains(bodyContent, `name="model"`)
			hasTargetFormatField := strings.Contains(bodyContent, `name="targetformat"`)

			fmt.Printf("=== FIELD VERIFICATION ===\n")
			fmt.Printf("Field count: %d (expected: 2)\n", fieldCount)
			fmt.Printf("Has 'model' field: %t\n", hasModelField)
			fmt.Printf("Has 'targetformat' field: %t\n", hasTargetFormatField)

			if fieldCount != 2 {
				fmt.Printf("❌ WRONG FIELD COUNT! Expected 2, got %d\n", fieldCount)
			} else if hasModelField && hasTargetFormatField {
				fmt.Printf("✅ Correct: Exactly 2 fields present\n")
			} else {
				fmt.Printf("❌ WRONG FIELDS! Missing expected fields\n")
			}

			// Extract and display field values
			fmt.Printf("=== FORM FIELDS ===\n")

			// Extract model filename
			modelFileName := "NOT_FOUND"
			if modelMatch := strings.Index(bodyContent, `name="model"`); modelMatch >= 0 {
				remaining := bodyContent[modelMatch:]
				if filenameStart := strings.Index(remaining, `filename="`); filenameStart >= 0 {
					filenameStart += 10
					if filenameEnd := strings.Index(remaining[filenameStart:], `"`); filenameEnd >= 0 {
						modelFileName = remaining[filenameStart : filenameStart+filenameEnd]
					}
				}
			}

			// Extract targetformat value
			targetFormatValue := "NOT_FOUND"
			if targetMatch := strings.Index(bodyContent, `name="targetformat"`); targetMatch >= 0 {
				remaining := bodyContent[targetMatch:]
				if valueStart := strings.Index(remaining, "\r\n\r\n"); valueStart >= 0 {
					valueStart += 4
					if valueEnd := strings.Index(remaining[valueStart:], "\r\n"); valueEnd >= 0 {
						targetFormatValue = remaining[valueStart : valueStart+valueEnd]
					}
				}
			}

			fmt.Printf("model: <%s>\n", modelFileName)
			fmt.Printf("targetformat: %s\n", targetFormatValue)
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

	filename := filepath.Base(filePath)

	return c.TransformModel(ctx, file, targetFormat, filename)
}

func (c *ModelTransformClient) TransformModelFromRequest(ctx context.Context, req *ModelTransformRequest) ([]byte, error) {
	return c.TransformModel(ctx, req.ModelFile, req.TargetFormat, req.FileName)
}
