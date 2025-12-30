package score

import "wifi-radar/internal/model"

func SampleScore(sample model.Sample) int {
	signalScore := scaleSignal(sample.SignalDBM)
	bitrateScore := scaleBitrate(sample.RxBitrateMbps + sample.TxBitrateMbps)
	return signalScore*2 + bitrateScore
}

func scaleSignal(signalDBM int) int {
	if signalDBM == 0 {
		return 0
	}
	// Map -100..-30 dBm to 0..100.
	val := (signalDBM + 100) * 100 / 70
	if val < 0 {
		return 0
	}
	if val > 100 {
		return 100
	}
	return val
}

func scaleBitrate(mbps float64) int {
	if mbps <= 0 {
		return 0
	}
	val := int(mbps / 10)
	if val > 100 {
		return 100
	}
	return val
}
