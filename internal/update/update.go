// Package update checks GitHub releases for a newer quarry and swaps the running
// binary for it. Works with private repos as long as a token is available.
package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const Repo = "ozhouso/Quarry"

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // api url, the one that works for private repos
	Size int64  `json:"size"`
}

type Release struct {
	Tag    string  `json:"tag_name"`
	Name   string  `json:"name"`
	Body   string  `json:"body"`
	Assets []Asset `json:"assets"`
}

var ErrNoAuth = errors.New("couldn't find a release. If the repo is still private set QUARRY_TOKEN (or GH_TOKEN) to a token with read access, or run `gh auth login`; otherwise no release has been published yet")

// Token looks for something to authenticate with, empty string if there isnt one
func Token() string {
	for _, k := range []string{"QUARRY_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func get(ctx context.Context, url, token, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "quarry-updater")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return http.DefaultClient.Do(req)
}

// Latest fetches the newest published release
func Latest(ctx context.Context, token string) (*Release, error) {
	resp, err := get(ctx, "https://api.github.com/repos/"+Repo+"/releases/latest", token, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusForbidden:
		if token == "" {
			return nil, ErrNoAuth
		}
		return nil, fmt.Errorf("github said %s (does the token have access to %s, and is there a release yet?)", resp.Status, Repo)
	default:
		return nil, fmt.Errorf("github said %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Newer says if tag is a higher version than current. 1.2.10 > 1.2.9, a leading v is ignored
func Newer(tag, current string) bool {
	a, b := parts(tag), parts(current)
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func parts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

// AssetName is what we expect the release binary for this machine to be called
func AssetName() string {
	n := fmt.Sprintf("quarry_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	return n
}

// Apply downloads the asset for this machine, checks it against checksums.txt and
// swaps it in for the running exe. progress gets called with bytes done / total
func Apply(ctx context.Context, rel *Release, token string, progress func(done, total int64)) error {
	want := AssetName()
	var bin, sums *Asset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case want:
			bin = &rel.Assets[i]
		case "checksums.txt":
			sums = &rel.Assets[i]
		}
	}
	if bin == nil {
		return fmt.Errorf("release %s has no %s", rel.Tag, want)
	}
	if sums == nil {
		return fmt.Errorf("release %s has no checksums.txt, refusing to install unverified", rel.Tag)
	}

	expected, err := expectedSum(ctx, sums, want, token)
	if err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	tmp := exe + ".new"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("can't write next to the exe (%w), try running from a folder you own", err)
	}
	ok := false
	defer func() {
		out.Close()
		if !ok {
			os.Remove(tmp)
		}
	}()

	resp, err := get(ctx, bin.URL, token, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}
	h := sha256.New()
	var done int64
	buf := make([]byte, 128<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if progress != nil {
				progress(done, bin.Size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != expected {
		return fmt.Errorf("checksum mismatch (got %s, release says %s), not installing", got[:12], expected[:12])
	}

	// windows won't let us overwrite a running exe but it does let us rename it
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp, exe); err != nil {
			os.Rename(old, exe) // put it back
			return err
		}
	} else if err := os.Rename(tmp, exe); err != nil {
		return err
	}
	ok = true
	return nil
}

func expectedSum(ctx context.Context, sums *Asset, name, token string) (string, error) {
	resp, err := get(ctx, sums.URL, token, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("couldn't fetch checksums.txt: %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", name)
}

// Cleanup removes the .old exe a previous windows update left behind
func Cleanup() {
	if runtime.GOOS != "windows" {
		return
	}
	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

type cache struct {
	Checked time.Time `json:"checked"`
	Latest  string    `json:"latest"`
}

func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "quarry", "update.json")
}

// Passive does the once-a-day background check. Returns the newer tag or "".
// Never errors out loud, a failed check shouldnt bother anyone
func Passive(current string) string {
	if os.Getenv("QUARRY_NO_UPDATE_CHECK") != "" {
		return ""
	}
	p := cachePath()
	var c cache
	if p != "" {
		if data, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(data, &c)
		}
	}
	if time.Since(c.Checked) > 24*time.Hour {
		// no token is fine once the repo is public, the api allows anonymous reads.
		// while its private this just 404s and we stay quiet
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		c.Checked = time.Now() // remember failures too so we dont retry every single run
		if rel, err := Latest(ctx, Token()); err == nil {
			c.Latest = rel.Tag
		}
		if p != "" && os.MkdirAll(filepath.Dir(p), 0o755) == nil {
			if data, err := json.Marshal(c); err == nil {
				_ = os.WriteFile(p, data, 0o644)
			}
		}
	}
	if c.Latest != "" && Newer(c.Latest, current) {
		return c.Latest
	}
	return ""
}
