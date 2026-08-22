package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type expectedFixture struct {
	ImplementationCommit string `json:"implementation_commit"`
	QRID                 uint64 `json:"qr_id"`
	SourceLinkID         uint64 `json:"source_link_id"`
	PublicURL            string `json:"public_url"`
	Destination          string `json:"destination"`
	Hostname             string `json:"hostname"`
	Code                 string `json:"code"`
	PNGSHA256            string `json:"png_sha256"`
	SVGSHA256            string `json:"svg_sha256"`
}

type decodedArtifact struct {
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
	Decoded string `json:"decoded"`
}

type evidence struct {
	Node                 string                     `json:"node"`
	CaseID               string                     `json:"case_id"`
	ImplementationCommit string                     `json:"implementation_commit"`
	Status               string                     `json:"status"`
	Driver               string                     `json:"driver"`
	Decoder              string                     `json:"decoder"`
	Rasterizer           string                     `json:"svg_rasterizer"`
	ExpectedPublicURL    string                     `json:"expected_public_url"`
	Artifacts            map[string]decodedArtifact `json:"artifacts"`
	RedirectFollow       map[string]any             `json:"redirect_follow"`
	Errors               []string                   `json:"errors"`
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func exactHead(root string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("GITHUB_SHA")); v != "" {
		return v, nil
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func decodePNG(path string) (string, error) {
	cmd := exec.Command("zbarimg", "--quiet", "--raw", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("zbarimg %s: %w: %s", path, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func decodeSVG(svgPath, pngPath string) (string, error) {
	cmd := exec.Command("rsvg-convert", "-o", pngPath, svgPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rsvg-convert: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return decodePNG(pngPath)
}

func followDecoded(decoded, destination string) (map[string]any, error) {
	parsed, err := url.Parse(decoded)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, fmt.Errorf("decoded payload is not authoritative https URL: %q", decoded)
	}
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("GOJET_TEST_REDIRECT_URL")), "/")
	if base == "" {
		base = "http://127.0.0.1:18080"
	}
	target := base + parsed.EscapedPath()
	if parsed.RawQuery != "" {
		target += "?" + parsed.RawQuery
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Host = parsed.Host
	req.Header.Set("User-Agent", "GoJet-P08-Independent-Scanner/1.0")
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	location := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("decoded URL did not traverse redirect authority: status=%d body=%q", resp.StatusCode, string(body))
	}
	if location != destination {
		return nil, fmt.Errorf("redirect Location=%q expected=%q", location, destination)
	}
	return map[string]any{
		"request_host":        parsed.Host,
		"request_path":        parsed.EscapedPath(),
		"status":              resp.StatusCode,
		"location":            location,
		"destination_matches": true,
	}, nil
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "--case" || os.Args[2] != "P08-T003" {
		fmt.Fprintln(os.Stderr, "usage: scanner_driver --case P08-T003")
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	head, err := exactHead(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	scannerDir := filepath.Join(root, "artifacts", "v10", "P08", "scanner")
	resultPath := filepath.Join(scannerDir, "P08-T003.json")
	fixturePath := filepath.Join(scannerDir, "P08-T003-expected.json")
	rawFixture, err := os.ReadFile(fixturePath)
	result := evidence{
		Node: "P08", CaseID: "P08-T003", ImplementationCommit: head,
		Status: "FAIL", Driver: "go run ./scripts/p08/scanner_driver --case P08-T003",
		Decoder:    "zbarimg (ZBar) independent from github.com/skip2/go-qrcode",
		Rasterizer: "rsvg-convert for downloaded SVG before ZBar decode",
		Artifacts:  map[string]decodedArtifact{}, Errors: []string{},
	}
	fail := func(e error) {
		result.Errors = append(result.Errors, e.Error())
		_ = os.MkdirAll(scannerDir, 0o755)
		encoded, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(resultPath, append(encoded, '\n'), 0o644)
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if err != nil {
		fail(err)
	}
	var fixture expectedFixture
	if err := json.Unmarshal(rawFixture, &fixture); err != nil {
		fail(err)
	}
	if fixture.ImplementationCommit != head {
		fail(fmt.Errorf("fixture head=%s expected exact head=%s", fixture.ImplementationCommit, head))
	}
	result.ExpectedPublicURL = fixture.PublicURL

	pngPath := filepath.Join(scannerDir, "P08-T003-source.png")
	pngRaw, err := os.ReadFile(pngPath)
	if err != nil {
		fail(err)
	}
	pngDigest := digest(pngRaw)
	if pngDigest != fixture.PNGSHA256 {
		fail(fmt.Errorf("PNG digest=%s expected=%s", pngDigest, fixture.PNGSHA256))
	}
	pngDecoded, err := decodePNG(pngPath)
	if err != nil {
		fail(err)
	}
	if pngDecoded != fixture.PublicURL {
		fail(fmt.Errorf("PNG decoded=%q expected=%q", pngDecoded, fixture.PublicURL))
	}
	result.Artifacts["png"] = decodedArtifact{Path: "artifacts/v10/P08/scanner/P08-T003-source.png", SHA256: pngDigest, Decoded: pngDecoded}

	svgPath := filepath.Join(scannerDir, "P08-T003-source.svg")
	svgRaw, err := os.ReadFile(svgPath)
	if err != nil {
		fail(err)
	}
	svgDigest := digest(svgRaw)
	if svgDigest != fixture.SVGSHA256 {
		fail(fmt.Errorf("SVG digest=%s expected=%s", svgDigest, fixture.SVGSHA256))
	}
	rasterPath := filepath.Join(scannerDir, "P08-T003-source-from-svg.png")
	svgDecoded, err := decodeSVG(svgPath, rasterPath)
	if err != nil {
		fail(err)
	}
	if svgDecoded != fixture.PublicURL {
		fail(fmt.Errorf("SVG decoded=%q expected=%q", svgDecoded, fixture.PublicURL))
	}
	result.Artifacts["svg"] = decodedArtifact{Path: "artifacts/v10/P08/scanner/P08-T003-source.svg", SHA256: svgDigest, Decoded: svgDecoded}

	follow, err := followDecoded(pngDecoded, fixture.Destination)
	if err != nil {
		fail(err)
	}
	result.RedirectFollow = follow
	result.Status = "PASS"
	result.Errors = []string{}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(resultPath, append(encoded, '\n'), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("P08-T003: PASS decoded %s via independent scanner and live redirect authority\n", fixture.PublicURL)
}
