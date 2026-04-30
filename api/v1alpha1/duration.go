// SPDX-License-Identifier:Apache-2.0

package v1alpha1

import "time"

func (n *Neighbor) HoldTime() *time.Duration {
	return durationFromPtr(n.HoldTimeSeconds, time.Second)
}

func (n *Neighbor) SetHoldTime(d *time.Duration) {
	n.HoldTimeSeconds = ptrFromDuration[int64](d, time.Second)
}

func (n *Neighbor) KeepaliveTime() *time.Duration {
	return durationFromPtr(n.KeepaliveTimeSeconds, time.Second)
}

func (n *Neighbor) SetKeepaliveTime(d *time.Duration) {
	n.KeepaliveTimeSeconds = ptrFromDuration[int64](d, time.Second)
}

func (n *Neighbor) ConnectTime() *time.Duration {
	return durationFromPtr(n.ConnectTimeSeconds, time.Second)
}

func (n *Neighbor) SetConnectTime(d *time.Duration) {
	n.ConnectTimeSeconds = ptrFromDuration[int64](d, time.Second)
}

func (b *BFDSettings) GetReceiveInterval() *time.Duration {
	return durationFromPtr(b.ReceiveInterval, time.Millisecond)
}

func (b *BFDSettings) SetReceiveInterval(d *time.Duration) {
	b.ReceiveInterval = ptrFromDuration[int32](d, time.Millisecond)
}

func (b *BFDSettings) GetTransmitInterval() *time.Duration {
	return durationFromPtr(b.TransmitInterval, time.Millisecond)
}

func (b *BFDSettings) SetTransmitInterval(d *time.Duration) {
	b.TransmitInterval = ptrFromDuration[int32](d, time.Millisecond)
}

func (b *BFDSettings) GetEchoInterval() *time.Duration {
	return durationFromPtr(b.EchoInterval, time.Millisecond)
}

func (b *BFDSettings) SetEchoInterval(d *time.Duration) {
	b.EchoInterval = ptrFromDuration[int32](d, time.Millisecond)
}

func durationFromPtr[T ~int32 | ~int64](v *T, unit time.Duration) *time.Duration {
	if v == nil {
		return nil
	}
	d := time.Duration(*v) * unit
	return &d
}

func ptrFromDuration[T ~int32 | ~int64](d *time.Duration, unit time.Duration) *T {
	if d == nil {
		return nil
	}
	v := T(*d / unit)
	return &v
}
