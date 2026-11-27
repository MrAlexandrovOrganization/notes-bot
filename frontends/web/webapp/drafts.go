package webapp

import "net/http"

// Explicit acknowledgement lets the browser discard a draft after its write
// even if refreshing the rendered note fails separately.
func confirmDraftWrite(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Notes-Draft") != "1" {
		return false
	}
	w.Header().Set("X-Notes-Draft", "saved")
	w.WriteHeader(http.StatusNoContent)
	return true
}
