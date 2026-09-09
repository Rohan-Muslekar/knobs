package api

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// pgxTx lets handlers reference the transaction type Repo.WithTx hands them
// without each importing pgx directly.
type pgxTx = pgx.Tx

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
