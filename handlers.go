package main

import (
	"net/http"
)

func (a *app) handleHealthz(w http.ResponseWriter, r *http.Request) {
	sqlDB, err := a.db.DB()
	if err == nil {
		err = sqlDB.PingContext(r.Context())
	}
	if err != nil {
		a.logger.Error("healthz: db ping failed", "err", err)
		http.Error(w, "db unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, http.StatusOK, "home.html", nil)
}
