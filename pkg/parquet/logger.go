// Package parquet persists captured request fingerprints to Parquet files.
//
// The logger is intentionally non-blocking: request handlers hand off rows over
// a buffered channel and a single background goroutine batches them into rolling
// Parquet files. Each flush produces a new file (data/trackme-<unixnano>.parquet),
// which keeps the writer simple (Parquet files are immutable once their footer is
// written) and yields a directory of files that analytics tools read as one
// dataset.
package parquet

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	pq "github.com/parquet-go/parquet-go"
	"github.com/pagpeter/trackme/pkg/types"
)

// Row is the flat schema written to Parquet. It captures the useful fingerprints
// rather than the full nested Response (whose extension list does not map cleanly
// onto a columnar schema).
type Row struct {
	Timestamp     int64  `parquet:"timestamp"`
	IP            string `parquet:"ip"`
	HTTPVersion   string `parquet:"http_version"`
	Method        string `parquet:"method"`
	Path          string `parquet:"path"`
	UserAgent     string `parquet:"user_agent"`
	JA3           string `parquet:"ja3"`
	JA3Hash       string `parquet:"ja3_hash"`
	JA4           string `parquet:"ja4"`
	JA4R          string `parquet:"ja4_r"`
	PeetPrint     string `parquet:"peetprint"`
	PeetPrintHash string `parquet:"peetprint_hash"`
	Akamai        string `parquet:"akamai"`
	AkamaiHash    string `parquet:"akamai_hash"`
}

const (
	// channelBuffer is how many rows can queue before logging starts dropping
	// them. Dropping (rather than blocking) keeps request handling fast even if
	// disk writes stall.
	channelBuffer = 4096
	// batchSize triggers a flush once this many rows have accumulated.
	batchSize = 1_000_000
	// flushInterval forces a flush of any buffered rows even when batchSize is
	// not reached, so low-traffic data is not held in memory indefinitely.
	flushInterval = 24 * time.Hour
)

// Logger persists Rows to Parquet files in a directory.
type Logger struct {
	dir    string
	logIPs bool

	ch   chan Row
	done chan struct{}
	wg   sync.WaitGroup
}

// New creates and starts a Logger writing to dir. The directory is created if it
// does not exist.
func New(dir string, logIPs bool) (*Logger, error) {
	if dir == "" {
		dir = "data"
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parquet dir %q: %w", dir, err)
	}

	l := &Logger{
		dir:    dir,
		logIPs: logIPs,
		ch:     make(chan Row, channelBuffer),
		done:   make(chan struct{}),
	}
	l.wg.Add(1)
	go l.run()
	return l, nil
}

// Log enqueues a captured Response. It never blocks: if the buffer is full the
// row is dropped and a warning is logged.
func (l *Logger) Log(res types.Response) {
	if l == nil {
		return
	}
	row := rowFromResponse(res, l.logIPs)
	select {
	case l.ch <- row:
	default:
		log.Println("parquet: buffer full, dropping row")
	}
}

// Close flushes any buffered rows and stops the background goroutine.
func (l *Logger) Close() {
	if l == nil {
		return
	}
	close(l.done)
	l.wg.Wait()
}

func rowFromResponse(res types.Response, logIPs bool) Row {
	row := Row{
		Timestamp:   time.Now().UnixNano(),
		HTTPVersion: res.HTTPVersion,
		Method:      res.Method,
		Path:        res.Path,
		UserAgent:   res.UserAgent,
	}
	if logIPs {
		row.IP = res.IP
	}
	if res.TLS != nil {
		row.JA3 = res.TLS.JA3
		row.JA3Hash = res.TLS.JA3Hash
		row.JA4 = res.TLS.JA4
		row.JA4R = res.TLS.JA4_r
		row.PeetPrint = res.TLS.PeetPrint
		row.PeetPrintHash = res.TLS.PeetPrintHash
	}
	if res.HTTPVersion == "h2" && res.Http2 != nil {
		row.Akamai = res.Http2.AkamaiFingerprint
		row.AkamaiHash = res.Http2.AkamaiFingerprintHash
	} else if res.HTTPVersion == "h3" && res.Http3 != nil {
		row.Akamai = res.Http3.AkamaiFingerprint
		row.AkamaiHash = res.Http3.AkamaiFingerprintHash
	}
	return row
}

func (l *Logger) run() {
	defer l.wg.Done()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	// Grow on demand rather than reserving batchSize rows up front, since
	// batchSize is large and most deployments will flush on the timer.
	buf := make([]Row, 0, 4096)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		if err := l.writeFile(buf); err != nil {
			log.Println("parquet: failed to write file:", err)
		}
		buf = buf[:0]
	}

	for {
		select {
		case row := <-l.ch:
			buf = append(buf, row)
			if len(buf) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-l.done:
			// Drain anything still queued, then flush and exit.
			for {
				select {
				case row := <-l.ch:
					buf = append(buf, row)
				default:
					flush()
					return
				}
			}
		}
	}
}

// writeFile writes a batch of rows to a new Parquet file. The file is written to
// a temporary name first and renamed on success so readers never observe a
// partially written file.
func (l *Logger) writeFile(rows []Row) error {
	name := fmt.Sprintf("trackme-%d.parquet", time.Now().UnixNano())
	final := filepath.Join(l.dir, name)
	tmp := final + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %q: %w", tmp, err)
	}

	w := pq.NewGenericWriter[Row](f, pq.Compression(&pq.Snappy))
	if _, err := w.Write(rows); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("write rows: %w", err)
	}
	if err := w.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("close writer: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close file: %w", err)
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %q: %w", tmp, err)
	}
	return nil
}
