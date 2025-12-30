package model

import "time"

type Sample struct {
	IfName         string  `json:"ifname"`
	SSID           string  `json:"ssid"`
	BSSID          string  `json:"bssid"`
	FreqMHz        int     `json:"freq_mhz"`
	SignalDBM      int     `json:"signal_dbm"`
	RxBitrateMbps  float64 `json:"rx_mbps"`
	TxBitrateMbps  float64 `json:"tx_mbps"`
	TimestampUnixM int64   `json:"ts_unix_ms"`
}

type Status struct {
	Interfaces []Sample `json:"interfaces"`
}

type Best struct {
	Sample Sample `json:"sample"`
	Score  int    `json:"score"`
}

func NowUnixMS() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
