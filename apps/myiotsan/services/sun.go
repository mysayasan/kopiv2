package services

import (
	"math"
	"time"
)

// Sunrise/sunset by the standard NOAA / "sunrise equation" algorithm — pure arithmetic, no
// dependency and no network. A schedule that fires "30 minutes before sunset" needs the sun's times
// for the site, and an air-gapped appliance cannot ask an API for them, so it computes them.
//
// Accuracy is ~1 minute at the mid-latitudes that matter here, which is far finer than a per-minute
// scheduler tick can act on. Longitude is EAST-positive (the geographic convention); latitude is
// north-positive. The returned times are UTC — the scheduler compares them against time.Now().

const sunDegRad = math.Pi / 180

// SunriseSunset returns the sunrise and sunset (UTC) for the calendar day of date at the given
// latitude/longitude. ok is false at a polar day/night, where the sun does not cross the horizon and
// neither time is defined.
func SunriseSunset(date time.Time, lat, lon float64) (rise, set time.Time, ok bool) {
	y, m, d := date.UTC().Date()
	jd := julianDay(y, int(m), d) // JD at 00:00 UTC of the day

	n := math.Ceil(jd - 2451545.0 + 0.0008)
	lw := -lon // the algorithm uses west-positive longitude
	jStar := n - lw/360.0

	mAnom := math.Mod(357.5291+0.98560028*jStar, 360) // solar mean anomaly, degrees
	c := 1.9148*sinDeg(mAnom) + 0.0200*sinDeg(2*mAnom) + 0.0003*sinDeg(3*mAnom)
	lambda := math.Mod(mAnom+c+180+102.9372, 360) // ecliptic longitude, degrees

	jTransit := 2451545.0 + jStar + 0.0053*sinDeg(mAnom) - 0.0069*sinDeg(2*lambda)
	sinDecl := sinDeg(lambda) * sinDeg(23.4397)
	decl := math.Asin(sinDecl) / sunDegRad // degrees

	// Hour angle for the geometric horizon with standard −0.833° refraction/disc correction.
	cosOmega := (sinDeg(-0.833) - sinDeg(lat)*sinDecl) / (cosDeg(lat) * cosDeg(decl))
	if cosOmega < -1 || cosOmega > 1 {
		return time.Time{}, time.Time{}, false // sun never rises or never sets this day
	}
	omega := math.Acos(cosOmega) / sunDegRad // degrees

	jRise := jTransit - omega/360.0
	jSet := jTransit + omega/360.0
	return fromJulian(jRise), fromJulian(jSet), true
}

func sinDeg(d float64) float64 { return math.Sin(d * sunDegRad) }
func cosDeg(d float64) float64 { return math.Cos(d * sunDegRad) }

// julianDay is the Julian Date at 00:00 UTC of a Gregorian calendar date.
func julianDay(y, m, d int) float64 {
	if m <= 2 {
		y--
		m += 12
	}
	a := y / 100
	b := 2 - a + a/4
	return math.Floor(365.25*float64(y+4716)) + math.Floor(30.6001*float64(m+1)) + float64(d) + float64(b) - 1524.5
}

// fromJulian converts a Julian Date to a UTC time, rounded to the nearest second.
func fromJulian(j float64) time.Time {
	unixSec := (j - 2440587.5) * 86400.0
	return time.Unix(int64(math.Round(unixSec)), 0).UTC()
}
