package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

var ErrResponseNotFound = errors.New("matching response not found")

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type HARFile struct {
	Log struct {
		Entries []Entry `json:"entries"`
	} `json:"log"`
}

type Entry struct {
	Request struct {
		Method string   `json:"method"`
		URL    string   `json:"url"`
		Headers []Header `json:"headers"`
	} `json:"request"`

	Response struct {
		Status  int      `json:"status"`
		Headers []Header `json:"headers"`
		Content Content  `json:"content"`
	} `json:"response"`
}

type Content struct {
	Text     string `json:"text"`
	Encoding string `json:"encoding"`
	MimeType string `json:"mimeType"`
}

type MatchedResponse struct {
	StatusCode      int
	Headers         []Header
	ContentType     string
	ContentEncoding string
	Body            []byte
}

func findResponse(target string) (*MatchedResponse, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, errors.New("target URL or path is empty")
	}

	matches, err := buildURLMatcher(target)
	if err != nil {
		return nil, err
	}

    directory := "static/test"
	files, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", directory, err)
	}

	for _, file := range files {
		if !file.Type().IsRegular() {
			continue
		}

		filePath := filepath.Join(directory, file.Name())

		har, err := readHARFile(filePath)
		if err != nil {
			return nil, err
		}

		for _, entry := range har.Log.Entries {
			if !matches(entry.Request.URL) {
				continue
			}

			body, err := decodeContent(entry.Response.Content)
			if err != nil {
				return nil, fmt.Errorf(
					"decode response content in %q for URL %q: %w",
					filePath,
					entry.Request.URL,
					err,
				)
			}

            headers,ctype,cencoding := filterResponseHeaders(entry.Response.Headers)
			return &MatchedResponse{
				StatusCode:      entry.Response.Status,
				Headers:         headers,
				ContentType:     ctype,
				ContentEncoding: cencoding,
				Body:            body,
			}, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrResponseNotFound, target)
}

func readHARFile(filePath string) (*HARFile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", filePath, err)
	}
	defer file.Close()

	var har HARFile

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&har); err != nil {
		return nil, fmt.Errorf("decode JSON file %q: %w", filePath, err)
	}

	return &har, nil
}

func buildURLMatcher(target string) (func(string) bool, error) {
	if isHTTPURL(target) {
		return func(candidate string) bool {
			return candidate == target
		}, nil
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("parse target path %q: %w", target, err)
	}

	if targetURL.IsAbs() || targetURL.Host != "" {
		return nil, fmt.Errorf("invalid target path %q", target)
	}

	if targetURL.Path == "" || !strings.HasPrefix(targetURL.Path, "/") {
		return nil, fmt.Errorf(
			"target path must start with '/': %q",
			target,
		)
	}

	targetPath := targetURL.EscapedPath()
	targetQuery := targetURL.RawQuery
	compareQuery := strings.Contains(target, "?")

	return func(candidate string) bool {
		candidateURL, err := url.Parse(candidate)
		if err != nil {
			return false
		}
		if !isHTTPURL(candidate) || candidateURL.Host == "" {
			return false
		}
		if candidateURL.EscapedPath() != targetPath {
			return false
		}
		if compareQuery && candidateURL.RawQuery != targetQuery {
			return false
		}

		return true
	}, nil
}

func isHTTPURL(value string) bool {
	lowerValue := strings.ToLower(value)
	return strings.HasPrefix(lowerValue, "http://") ||
		strings.HasPrefix(lowerValue, "https://")
}

func filterResponseHeaders(headers []Header) ([]Header, string, string) {
    ctype := ""
    cencoding := ""
	filtered := make([]Header, 0, len(headers))
	for _, header := range headers {
		name := strings.TrimSpace(header.Name)
		if strings.HasPrefix(name, ":") {
			continue
		}
		if strings.EqualFold(name, "content-encoding") {
		    cencoding = header.Value
			continue
		}
        if strings.EqualFold(name, "content-type") {
            ctype = header.Value
            continue
    	}
		filtered = append(filtered, header)
	}

	return filtered,ctype,cencoding
}

func decodeContent(content Content) ([]byte, error) {
	if !strings.EqualFold(content.Encoding, "base64") {
		return []byte(content.Text), nil
	}
	decoded, err := base64.StdEncoding.DecodeString(content.Text)
	if err == nil {
		return decoded, nil
	}
	decoded, rawErr := base64.RawStdEncoding.DecodeString(content.Text)
	if rawErr != nil {
		return nil, fmt.Errorf(
			"invalid base64 content: standard=%v, raw=%v",
			err,
			rawErr,
		)
	}

	return decoded, nil
}