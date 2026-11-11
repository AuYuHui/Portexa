package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"portexa/mt4/internal/verification"
)

func run() error {
	path := flag.String("vector", "", "path to a private captured vector; no network request is made")
	flag.Parse()
	if *path == "" {
		return errors.New("--vector is required")
	}
	var input struct {
		Target   string `json:"target"`
		Server   string `json:"server"`
		Login    uint32 `json:"login"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return errors.New("cannot decode local verification credentials")
	}
	if input.Login == 0 || len(input.Password) < 1 || len(input.Password) > 16 || strings.ContainsRune(input.Password, 0) {
		return errors.New("invalid local verification credentials")
	}
	file, err := os.Open(*path)
	if err != nil {
		return errors.New("cannot read private vector")
	}
	defer file.Close()
	var vector verification.Vector
	if err := json.NewDecoder(io.LimitReader(file, 64<<20)).Decode(&vector); err != nil {
		return errors.New("cannot decode private vector")
	}
	result, err := verification.Verify(verification.Credentials{Login: input.Login, Password: input.Password, Server: input.Server}, vector)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
