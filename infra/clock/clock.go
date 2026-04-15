package clock

import "time"

type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
}

type System struct{}

func (System) Now() time.Time        { return time.Now() }
func (System) Sleep(d time.Duration) { time.Sleep(d) }
