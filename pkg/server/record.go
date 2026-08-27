package server

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/pagpeter/trackme/pkg/types"
)

const (
	DirName  = "requests-db"
	FileName = "records.fpr"
)

var mu sync.Mutex

func recordDbPath() (string, error) {
	if err := os.MkdirAll(DirName, 0755); err != nil {
		return "", err
	}
	return filepath.Join(DirName, FileName), nil
}

func recordResponse(path string, resp types.Response) error {
	filePath, err := recordDbPath()
	if err != nil {
		return err
	}

	mu.Lock()
	defer mu.Unlock()

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
    sum := md5.Sum([]byte(string([]rune(path)[:])))
	hash := hex.EncodeToString(sum[:])
	line := hash + string(data) + "\n"

	_, err = f.WriteString(line)
	return err
}

func processPath(path string, deleteKey string) ([]string, bool, error) {
	filePath, err := recordDbPath()
	if err != nil {
		return nil,false, err
	}

	mu.Lock()
	defer mu.Unlock()

	if strings.HasPrefix(path, "/show/") {
		arg := strings.TrimPrefix(path, "/show/")
		if lineNum, err := strconv.Atoi(arg); err == nil {
			if lineNum < 1 {
				return nil,false, errors.New("row ID must be >= 1")
			}
			return readSpecificRow(filePath, lineNum)
		}
        arg = "/" + arg
        sum := md5.Sum([]byte(arg))
		targetHash := hex.EncodeToString(sum[:])
		return findByHash(filePath, targetHash)
	}

	if strings.HasPrefix(path, "/"+deleteKey+"/") {
		arg := strings.TrimPrefix(path, "/"+deleteKey+"/")
		if arg == "" {
			return nil,false, errors.New("missing delete argument")
		}

		if strings.EqualFold(arg, "all") {
			if err := os.WriteFile(filePath, []byte{}, 0644); err != nil {
				return nil,false, err
			}
			return nil,true, nil
		}

		count, err := strconv.Atoi(arg)
		if err != nil || count < 1 {
			return nil,false, errors.New("invalid delete count")
		}

		return deleteFirstN(filePath, count)
	}

	return []string{},false, nil
}

func readSpecificRow(filePath string, targetLine int) ([]string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil,false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	current := 0

	for scanner.Scan() {
		current++
		if current == targetLine {
			line := scanner.Text()
			if len(line) < 32 {
				return nil,false, errors.New("corrupted data line")
			}
			return []string{line[32:]},false, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil,false, err
	}

	return nil,false, errors.New("line index out of range")
}

func findByHash(filePath string, targetHash string) ([]string, bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil,false, errors.New("data file not found")
		}
		return nil,false, err
	}
	defer file.Close()

	var results []string
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) >= 32 {
			hash := line[:32]
			if strings.EqualFold(hash, targetHash) {
				results = append(results, line[32:])
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil,false, err
	}

	if len(results) == 0 {
		return nil,false, errors.New("no matching records found")
	}

	return results,false, nil
}

func deleteFirstN(filePath string, count int) ([]string, bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil,false, err
	}

	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	if count > len(lines) {
		count = len(lines)
	}

	remaining := strings.Join(lines[count:], "")
	if err := os.WriteFile(filePath, []byte(remaining), 0644); err != nil {
		return nil,false, err
	}

	return nil,true, nil
}
