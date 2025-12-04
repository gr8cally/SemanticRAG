package main

import (
	"encoding/json"
	"net/http"
)

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement upload handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Upload endpoint not yet implemented"})
}

func promptHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement prompt handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Prompt endpoint not yet implemented"})
}

func rechunkHandler(w http.ResponseWriter, r *http.Request) {
	// TODO: implement rechunk handling
	json.NewEncoder(w).Encode(map[string]string{"message": "Rechunk endpoint not yet implemented"})
	w.Write([]byte("Yello ma guy"))
}

func main() {
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/prompt", promptHandler)
	http.HandleFunc("/rechunk", rechunkHandler)

	http.ListenAndServe(":8081", nil)
}
