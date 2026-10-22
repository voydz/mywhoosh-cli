package mywhoosh

import "net/http"

type Client struct {
	HTTP *http.Client
	DownloadHTTP *http.Client
	LoginURL string
	ActivitiesURL string
	DownloadURL string
}

func New() *Client { return &Client{} }
