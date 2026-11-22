package webapp

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type appRelease struct {
	PackageID         string `json:"package_id"`
	VersionCode       int64  `json:"version_code"`
	VersionName       string `json:"version_name"`
	SHA256            string `json:"sha256"`
	Bytes             int64  `json:"bytes"`
	CertificateSHA256 string `json:"certificate_sha256"`
	Revision          string `json:"revision"`
	MinSDK            int    `json:"min_sdk"`
}

var releaseHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Only the current, atomically published release is available. Clients cannot
// choose filesystem paths or forward their bearer through redirects.
func (a *App) registerAppUpdates(mux *http.ServeMux) {
	directory := a.Cfg.MobileReleaseDir
	read := func() (appRelease, error) {
		var release appRelease
		f, err := os.Open(filepath.Join(directory, "current.json"))
		if err != nil {
			return release, err
		}
		defer f.Close()
		if err = json.NewDecoder(io.LimitReader(f, 8193)).Decode(&release); err != nil {
			return release, err
		}
		if !releaseHash.MatchString(release.SHA256) || !releaseHash.MatchString(release.CertificateSHA256) || release.PackageID != "ru.mralexandrov.notes" || release.VersionCode <= 0 || release.VersionCode > 2100000000 || release.Bytes <= 0 || release.Bytes > 64*1024*1024 || release.MinSDK < 26 || len(release.VersionName) == 0 || len(release.VersionName) > 100 {
			return appRelease{}, os.ErrInvalid
		}
		return release, nil
	}
	mux.HandleFunc("GET /mobile/v1/app-update", func(w http.ResponseWriter, r *http.Request) {
		release, err := read()
		if os.IsNotExist(err) {
			w.WriteHeader(204)
			return
		}
		if err != nil {
			mobileError(w, 503)
			return
		}
		mobileJSON(w, 200, release)
	})
	mux.HandleFunc("GET /mobile/v1/app-update/{file}", func(w http.ResponseWriter, r *http.Request) {
		release, err := read()
		if err != nil || r.PathValue("file") != release.SHA256+".apk" {
			w.WriteHeader(404)
			return
		}
		f, err := os.Open(filepath.Join(directory, release.SHA256+".apk"))
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() != release.Bytes {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(2 * time.Minute))
		http.ServeContent(w, r, "notes.apk", info.ModTime(), f)
	})
}
