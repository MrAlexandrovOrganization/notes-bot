package webapp

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMobileUpdatesAreAuthenticatedAndBoundToCurrentArtifact(t *testing.T) {
	a := testApp(t)
	a.Cfg.MobileReleaseDir = t.TempDir()
	h := a.NewMobileRouter()
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	path := "/mobile/v1/app-update"
	require.Equal(t, 401, mobileRequest(h, "GET", path, "", "").Code)
	require.Equal(t, 204, mobileRequest(h, "GET", path, token, "").Code)
	hash := strings.Repeat("a", 64)
	release := appRelease{PackageID: "ru.mralexandrov.notes", VersionCode: 2, VersionName: "1.1.0", SHA256: hash, Bytes: 3, CertificateSHA256: strings.Repeat("b", 64), MinSDK: 26}
	data, err := json.Marshal(release)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(a.Cfg.MobileReleaseDir, "current.json"), data, 0600))
	require.NoError(t, os.WriteFile(filepath.Join(a.Cfg.MobileReleaseDir, hash+".apk"), []byte("apk"), 0600))
	require.Equal(t, 200, mobileRequest(h, "GET", path, token, "").Code)
	require.Equal(t, 401, mobileRequest(h, "GET", path+"/"+hash+".apk", "", "").Code)
	require.Equal(t, 404, mobileRequest(h, "GET", path+"/other.apk", token, "").Code)
	w := mobileRequest(h, "GET", path+"/"+hash+".apk", token, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "apk", w.Body.String())
	require.NoError(t, os.WriteFile(filepath.Join(a.Cfg.MobileReleaseDir, hash+".apk"), []byte("truncated"), 0600))
	require.Equal(t, 503, mobileRequest(h, "GET", path+"/"+hash+".apk", token, "").Code)
	release.PackageID = "ru.mralexandrov.locations"
	data, err = json.Marshal(release)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(a.Cfg.MobileReleaseDir, "current.json"), data, 0600))
	require.Equal(t, 503, mobileRequest(h, "GET", path, token, "").Code)
}
