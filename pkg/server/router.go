package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/pagpeter/trackme/pkg/tls"
	"github.com/pagpeter/trackme/pkg/types"
	"github.com/pagpeter/trackme/pkg/utils"
)

func Log(msg string) {
	t := time.Now()
	formatted := t.Format("2006-01-02 15:04:05")
	fmt.Printf("[%v] %v\n", formatted, msg)
}

func cleanIP(ip string) string {
	return strings.Replace(strings.Replace(ip, "]", "", -1), "[", "", -1)
}

// Router returns bytes, content type, and error that should be sent to the client
func Router(path string, res types.Response, srv *Server) ([]byte, string, error) {
	if v, ok := srv.GetTCPFingerprints().Load(res.IP); ok {
		res.TCPIP = v.(types.TCPIPDetails)
	}
	res.Time = time.Now().UTC().Format("2006/01/02T15:04:05.000Z")
	if res.TLS != nil {
		// Use QUIC JA4 for HTTP/3 connections
		if res.HTTPVersion == "h3" {
			res.TLS.JA4 = tls.CalculateJa4QUIC(res.TLS)
			res.TLS.JA4_r = tls.CalculateJa4QUIC_r(res.TLS)
		} else {
			res.TLS.JA4 = tls.CalculateJa4(res.TLS)
			res.TLS.JA4_r = tls.CalculateJa4_r(res.TLS)
		}
		Log(fmt.Sprintf("%v %v %v %v %v", cleanIP(res.IP), res.Method, res.HTTPVersion, res.Path, res.TLS.JA3Hash))
	} else {
		Log(fmt.Sprintf("%v %v %v %v %v", cleanIP(res.IP), res.Method, res.HTTPVersion, res.Path, "-"))
	}

	u, err := url.Parse("https://tls.peet.ws" + path)
	var m map[string][]string
	if err != nil || u == nil {
		m = make(map[string][]string)
	} else {
		m, err = url.ParseQuery(u.RawQuery)
		if err != nil {
			m = make(map[string][]string)
		}
	}

    if u != nil {
        msg,del,err := processPath(u.Path, srv.GetConfig().DeleteKey)
        if err != nil {
            jsonErr := fmt.Sprintf(`{"error": %q}`, err.Error())
            return []byte(jsonErr), "application/json", nil
        }
        if del {
            return []byte(`{"ok": true}`), "application/json", nil
        }
        if len(msg) != 0 {
            rawJSONArray := "[" + strings.Join(msg, ",") + "]"
            var prettyJSON bytes.Buffer
            err := json.Indent(&prettyJSON, []byte(rawJSONArray), "", "  ")
            if err != nil {
                return []byte{},"",fmt.Errorf("Error json syntax:", err)
            }
            return []byte(prettyJSON.String()), "application/json", nil
        }
        if u.Path == "/favicon.ico" {
            b, err := utils.ReadFile("static/favicon.ico")
            if err != nil {
                return []byte{}, "text/html", nil
            }
            return []byte(b), "image/x-icon", nil
        }
        if srv.GetConfig().LogToDb {
            recordResponse(u.Path, res)
        }
        return apiAll(res, m)
    }
    if (true) {
       return []byte{}, "text/html", nil
    }

	paths := getAllPaths()
	if u != nil {
		if val, ok := paths[u.Path]; ok {
			return val(res, m)
		}
	}
	// 404
	b, err := utils.ReadFile("static/404.html")
	if err != nil {
		return []byte(`{"error": "page not found"}`), "application/json", nil
	}
	return []byte(b), "text/html", nil
}
