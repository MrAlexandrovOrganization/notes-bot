package webapp

import (
	"net/http"
	"net/url"
	"path/filepath"

	"notes-bot/frontends/web/views"
)

func (a *App) registerBrowseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /browse", a.handleBrowseFolder)
	mux.HandleFunc("GET /browse/file", a.handleBrowseFile)
	mux.HandleFunc("POST /browse/file/append", a.handleBrowseFileAppend)
}

func (a *App) handleBrowseFolder(w http.ResponseWriter, r *http.Request) {
	relpath := r.URL.Query().Get("path")
	entries, err := a.Core.ListDirectory(r.Context(), relpath)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	items := make([]views.BrowseEntry, len(entries))
	for i, e := range entries {
		items[i] = views.BrowseEntry{Name: e.Name, Relpath: e.Relpath, IsDir: e.IsDir}
	}

	parent := filepath.Dir(relpath)
	if parent == "." {
		parent = ""
	}

	a.render(w, r, views.Browse(views.BrowseData{
		Path:      relpath,
		ParentURL: parent,
		ShowUp:    relpath != "",
		Entries:   items,
	}))
}

func (a *App) handleBrowseFile(w http.ResponseWriter, r *http.Request) {
	relpath := r.URL.Query().Get("path")
	if relpath == "" {
		http.NotFound(w, r)
		return
	}
	content, err := a.Core.GetNoteByPath(r.Context(), relpath)
	if err != nil {
		a.serverError(w, r, err)
		return
	}

	parent := filepath.Dir(relpath)
	if parent == "." {
		parent = ""
	}

	a.render(w, r, views.BrowseFile(views.BrowseFileData{
		Name:      filepath.Base(relpath),
		Relpath:   relpath,
		Content:   truncatePreview(content),
		FolderURL: parent,
	}))
}

func (a *App) handleBrowseFileAppend(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	relpath := r.PostFormValue("relpath")
	text := r.PostFormValue("text")
	if relpath == "" || text == "" {
		a.formError(w, r, "Текст не может быть пустым")
		return
	}
	if _, err := a.Core.AppendToNoteByPath(r.Context(), relpath, text); err != nil {
		a.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/browse/file?path="+url.QueryEscape(relpath), http.StatusSeeOther)
}
