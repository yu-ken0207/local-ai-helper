package pairing

import (
	"encoding/base64"
	"net/url"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"
)

// Payload QR code 的內容：localai://pair?v=1&id=...&name=...&port=11480&code=123456&ip=...&ip=...
// App 掃到後依序試每個位址的 /helper/v1/hello（識別碼要對），再用配對碼配對。
func Payload(id, name string, port int, code string, ips []string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("id", id)
	q.Set("name", name)
	q.Set("port", strconv.Itoa(port))
	q.Set("code", code)
	for _, ip := range ips {
		q.Add("ip", ip)
	}
	return "localai://pair?" + q.Encode()
}

// QRDataURL 把內容畫成 QR code，回傳可以直接放進 <img> 的 data URL
func QRDataURL(content string) (string, error) {
	png, err := qrcode.Encode(content, qrcode.Medium, 320)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
