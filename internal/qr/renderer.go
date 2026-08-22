package qrcodes

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

var ErrUnsupportedFormat = errors.New("unsupported qr format")

type Artifact struct {
	Format      string
	ContentType string
	Bytes       []byte
	SHA256      string
}

func Render(target, format string) (Artifact, error) {
	target = strings.TrimSpace(target)
	format = strings.ToLower(strings.TrimSpace(format))
	if target == "" {
		return Artifact{}, ErrInvalidInput
	}
	qr, err := qrcode.New(target, qrcode.Medium)
	if err != nil {
		return Artifact{}, err
	}
	qr.DisableBorder = false

	var raw []byte
	var contentType string
	switch format {
	case "png":
		raw, err = qr.PNG(256)
		contentType = "image/png"
	case "svg":
		raw, err = renderSVG(qr.Bitmap())
		contentType = "image/svg+xml; charset=utf-8"
	default:
		return Artifact{}, ErrUnsupportedFormat
	}
	if err != nil {
		return Artifact{}, err
	}
	digest := sha256.Sum256(raw)
	return Artifact{Format: format, ContentType: contentType, Bytes: raw, SHA256: hex.EncodeToString(digest[:])}, nil
}

func renderSVG(bitmap [][]bool) ([]byte, error) {
	if len(bitmap) == 0 || len(bitmap[0]) == 0 {
		return nil, errors.New("empty qr bitmap")
	}
	size := len(bitmap)
	for _, row := range bitmap {
		if len(row) != size {
			return nil, errors.New("non-square qr bitmap")
		}
	}
	var path bytes.Buffer
	for y, row := range bitmap {
		for x, dark := range row {
			if !dark {
				continue
			}
			fmt.Fprintf(&path, "M%d %dh1v1h-1z", x, y)
		}
	}
	var out bytes.Buffer
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?>`)
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="256" height="256" role="img" aria-label="%s">`, size, size, html.EscapeString("GoJet QR code"))
	fmt.Fprintf(&out, `<rect width="100%%" height="100%%" fill="#fff"/>`)
	fmt.Fprintf(&out, `<path d="%s" fill="#000" shape-rendering="crispEdges"/>`, path.String())
	out.WriteString(`</svg>`)
	return out.Bytes(), nil
}
