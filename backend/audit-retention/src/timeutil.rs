// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

//! Minimal UTC time handling: RFC 3339 parsing and formatting (Go
//! `time.RFC3339Nano` form) and calendar-month subtraction with Go's
//! `time.AddDate` normalisation. Times are nanoseconds since the Unix epoch.

use crate::Result;

pub const NANOS_PER_SEC: i128 = 1_000_000_000;
pub const NANOS_PER_DAY: i128 = 86_400 * NANOS_PER_SEC;

pub fn now_nanos() -> i128 {
    let d = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default();
    d.as_nanos() as i128
}

/// Days since 1970-01-01 for a proleptic Gregorian date (H. Hinnant).
pub fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y >= 0 { y } else { y - 399 } / 400;
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146_097 + doe - 719_468
}

pub fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719_468;
    let era = if z >= 0 { z } else { z - 146_096 } / 146_097;
    let doe = z - era * 146_097;
    let yoe = (doe - doe / 1460 + doe / 36_524 - doe / 146_096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

/// Parses `YYYY-MM-DDTHH:MM:SS[.fraction](Z|±HH:MM)`.
pub fn parse_rfc3339(s: &str) -> Result<i128> {
    let b = s.as_bytes();
    let bad = || format!("invalid RFC 3339 time {s:?}");
    if b.len() < 20
        || b[4] != b'-'
        || b[7] != b'-'
        || (b[10] != b'T' && b[10] != b't')
        || b[13] != b':'
        || b[16] != b':'
    {
        return Err(bad().into());
    }
    let num = |r: std::ops::Range<usize>| -> Result<i64> {
        let t = &s[r];
        if !t.bytes().all(|c| c.is_ascii_digit()) {
            return Err(bad().into());
        }
        Ok(t.parse::<i64>()?)
    };
    let (y, mo, d, h, mi, se) = (
        num(0..4)?,
        num(5..7)?,
        num(8..10)?,
        num(11..13)?,
        num(14..16)?,
        num(17..19)?,
    );
    if !(1..=12).contains(&mo) || d < 1 || d > days_in_month(y, mo) || h > 23 || mi > 59 || se > 59
    {
        return Err(bad().into());
    }
    let mut i = 19;
    let mut frac: i128 = 0;
    if i < b.len() && b[i] == b'.' {
        i += 1;
        let start = i;
        while i < b.len() && b[i].is_ascii_digit() {
            i += 1;
        }
        let digits = &s[start..i];
        if digits.is_empty() || digits.len() > 9 {
            return Err(bad().into());
        }
        frac = format!("{digits:0<9}").parse::<i128>()?;
    }
    let offset: i128 = match &s[i..] {
        "Z" | "z" => 0,
        rest if rest.len() == 6
            && (rest.starts_with('+') || rest.starts_with('-'))
            && &rest[3..4] == ":" =>
        {
            let oh = num(i + 1..i + 3)? as i128;
            let om = num(i + 4..i + 6)? as i128;
            let sign = if rest.starts_with('-') { -1 } else { 1 };
            sign * (oh * 3600 + om * 60)
        }
        _ => return Err(bad().into()),
    };
    let days = days_from_civil(y, mo, d) as i128;
    let secs = days * 86_400 + (h * 3600 + mi * 60 + se) as i128 - offset;
    Ok(secs * NANOS_PER_SEC + frac)
}

/// Formats as Go's `t.UTC().Format(time.RFC3339Nano)`.
pub fn format_rfc3339(nanos: i128) -> String {
    let secs = nanos.div_euclid(NANOS_PER_SEC);
    let frac = nanos.rem_euclid(NANOS_PER_SEC);
    let days = secs.div_euclid(86_400) as i64;
    let sod = secs.rem_euclid(86_400) as i64;
    let (y, m, d) = civil_from_days(days);
    let mut out = format!(
        "{y:04}-{m:02}-{d:02}T{:02}:{:02}:{:02}",
        sod / 3600,
        sod % 3600 / 60,
        sod % 60
    );
    if frac != 0 {
        let f = format!("{frac:09}");
        out.push('.');
        out.push_str(f.trim_end_matches('0'));
    }
    out.push('Z');
    out
}

pub fn is_leap(y: i64) -> bool {
    (y % 4 == 0 && y % 100 != 0) || y % 400 == 0
}

pub fn days_in_month(y: i64, m: i64) -> i64 {
    match m {
        1 | 3 | 5 | 7 | 8 | 10 | 12 => 31,
        4 | 6 | 9 | 11 => 30,
        _ if is_leap(y) => 29,
        _ => 28,
    }
}

/// `t` minus `months` calendar months, normalised like Go's
/// `t.AddDate(0, -months, 0)` (31 March minus one month is 3 March in a
/// common year). Time of day is preserved.
pub fn sub_months(nanos: i128, months: u32) -> i128 {
    let secs = nanos.div_euclid(NANOS_PER_SEC);
    let frac = nanos.rem_euclid(NANOS_PER_SEC);
    let days = secs.div_euclid(86_400) as i64;
    let sod = secs.rem_euclid(86_400);
    let (y, m, d) = civil_from_days(days);
    let total = y * 12 + (m - 1) - months as i64;
    let (ny, nm) = (total.div_euclid(12), total.rem_euclid(12) + 1);
    // Day overflow rolls into the following month, as in Go.
    let nd = days_from_civil(ny, nm, 1) + (d - 1);
    (nd as i128 * 86_400 + sod) * NANOS_PER_SEC + frac
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_go_rfc3339nano() {
        for s in [
            "2026-01-02T03:04:05Z",
            "2026-01-02T03:04:05.1Z",
            "2026-12-31T23:59:59.123456789Z",
            "1970-01-01T00:00:00Z",
            "2024-02-29T12:00:00.000001Z",
        ] {
            assert_eq!(format_rfc3339(parse_rfc3339(s).unwrap()), s);
        }
        assert_eq!(
            parse_rfc3339("2026-01-02T05:04:05+02:00").unwrap(),
            parse_rfc3339("2026-01-02T03:04:05Z").unwrap()
        );
        for bad in [
            "",
            "2026-13-01T00:00:00Z",
            "2026-02-29T00:00:00Z",
            "2026-01-01 00:00:00Z",
            "2026-01-01T00:00:00",
            "2026-01-01T00:00:00.Z",
            "2026-01-01T00:00:00.1234567890Z",
        ] {
            assert!(parse_rfc3339(bad).is_err(), "{bad}");
        }
    }

    /// Calendar months with Go `AddDate` normalisation (reviewer probe F).
    #[test]
    fn calendar_months_match_go_adddate() {
        let t = |s| parse_rfc3339(s).unwrap();
        assert_eq!(
            format_rfc3339(sub_months(t("2026-11-15T00:00:00Z"), 60)),
            "2021-11-15T00:00:00Z"
        );
        assert_eq!(
            format_rfc3339(sub_months(t("2026-03-31T10:00:00Z"), 1)),
            "2026-03-03T10:00:00Z"
        );
        assert_eq!(
            format_rfc3339(sub_months(t("2028-02-29T00:00:00Z"), 12)),
            "2027-03-01T00:00:00Z"
        );
        assert_eq!(
            format_rfc3339(sub_months(t("2026-01-15T00:00:00.5Z"), 13)),
            "2024-12-15T00:00:00.5Z"
        );
        // 60 calendar months is never shorter than 60 x 30 days (the defect in cd05c1af).
        let now = t("2026-11-15T00:00:00Z");
        assert!(sub_months(now, 60) < now - 60 * 30 * NANOS_PER_DAY);
    }
}
