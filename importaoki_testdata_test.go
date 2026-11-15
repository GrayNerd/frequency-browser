package main

import (
	"encoding/base64"
	"testing"
)

// aokiTestPage mirrors the newsletter markup: the real page is a frameset, the
// frame document lists the archives newest-first and labels them separately from
// the link text. The last entry is deliberately labelled with an older season
// than the file it points at, which is what the real page does.
const aokiTestFramePage = `<html><body>
<a href="https://www1.m2.mediacat.ne.jp/binews/ut/xt/xta26.zip"><span><b>A26</b></span>shortwave schedule ziped file</a><br>
<a href="https://www1.m2.mediacat.ne.jp/binews/us/nz/nzb25.zip"><span><b>B25</b></span>shortwave schedule ziped file</a><br>
<a href="https://www1.s2.starcat.ne.jp/ndxc/pc/ns/nsa25.zip"><span><b>A25</b></span>shortwave schedule ziped file</a><br>
<a href="https://www1.s2.starcat.ne.jp/ndxc/pc/nc/ncb24.zip"><b><span>B24</span></b>shortwave schedule ziped file</a><br>
<a href="https://www1.m2.mediacat.ne.jp/binews/us/xz/xzb26.zip"><span><b>B22</b></span>shortwave schedule ziped file</a><br>
<a href="https://www1.s2.starcat.ne.jp/ndxc/pc/ns/userlist1.txt">userlist1.txt</a><br>
</body></html>`

// aokiTestFrameset is the site root: it carries no archive links of its own.
const aokiTestFrameset = `<html><frameset rows="*">
 <frame src="nnk.htm" name="topFrame">
</frameset></html>`

// The row fixtures below are captured verbatim from a real xta26.txt, so the
// fixed-width columns are byte-exact (Shift_JIS, CRLF). They are base64 encoded
// to keep the source file ASCII-safe and immune to editor re-encoding.
//
//	rowTimeSignal  "   40 Time Signal ... J   372221N1405056E NICT"
//	rowAirDelhi    "  810 AIR Delhi A (DRM)F250001 ... IND 284609N0770813E"
//	rowPbsXian     "  810 PBS Xi'an News Service ... CHN 341143N1085927E PBS"
//	rowPbsZhejiang "  810 PBS Zhejiang ... Yuhang Hangzhou<CJK> CHN ..."
//	rowLongLocation "  954 Country Radio ... Ceske Bud<0x3f>jovice/HusovaCZE 485920N..."
//	rowShortTail   " 6210 Pirate Radio ... 0.1  ND"  (no ADM/L-L columns)
//	rowVeryShort   "11945 Radio Barandazi 1330-1430   36    Persian"
const (
	aokiRowTimeSignal   = "ICAgNDAgVGltZSBTaWduYWwgICAgICAgICAgICAgICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBBMUIgICAgICAgICAgICAgICAgICAgNTAgIE5EIE90YWthZG95YW1hIFRhbXVyYSBDICAgIEogICAzNzIyMjFOMTQwNTA1NkUgTklDVA=="
	aokiRowAirDelhi     = "ICA4MTAgQUlSIERlbGhpIEEgKERSTSlGMjUwMDAxICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBIaW5kKERpZ2l0YWwpICAgICAgICAzMDAgICAgIERlbGhpICAgICAgICAgICAgICAgICAgIElORCAyODQ2MDlOMDc3MDgxM0UgQUlSIE5ld3MyNHg3"
	aokiRowPbsXian      = "ICA4MTAgUEJTIFhpJ2FuIE5ld3MgU2VydmljZSAgICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBDaGluZXNlICAgICAgICAgICAgICAgNTAgIE5EIFlhbnRhIFhpJ2FuIFNoYWFueGkgICAgIENITiAzNDExNDNOMTA4NTkyN0UgUEJT"
	aokiRowPbsZhejiang  = "ICA4MTAgUEJTIFpoZWppYW5nICAgICAgICAgICAgICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBDaGluZXNlICAgICAgICAgICAgICAyMDAgICAgIFl1aGFuZyBIYW5nemhvdY1Zj0KOcyAgIENITiAzMDI0NDVOMTIwMDM0N0UgUEJT"
	aokiRowLongLocation = "ICA5NTQgQ291bnRyeSBSYWRpbyAgICAgICAgICAgICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBDemVjaCAgICAgICAgICAgICAgICAgIDUgICAgIENlc2tlIEJ1ZD9qb3ZpY2UvSHVzb3ZhQ1pFIDQ4NTkyME4wMTQyOTM3RQ=="
	aokiRowShortTail    = "IDYyMTAgUGlyYXRlIFJhZGlvICAgICAgICAgICAgICAgICAgICAwMDAwLTI0MDAgMTIzNDU2NyBEdXRjaCAgICAgICAgICAgICAgICAwLjEgIE5E"
	aokiRowVeryShort    = "MTE5NDUgUmFkaW8gQmFyYW5kYXppICAgICAgICAgICAgICAgICAxMzMwLTE0MzAgICAzNiAgICBQZXJzaWFu"
)

const aokiTestTitle = "A26 Shortwave Frequecy List  October 23 2026,  1000 UTC   Day 1 = Sunday\r\n"
const aokiTestHeading = "FRE   STATION                         UTC       Su-W-Sa Language             Pow Azi Location                ADM L/L             Remarks\r\n"

// aokiTestSchedule assembles a schedule file from the given encoded rows.
func aokiTestSchedule(t *testing.T, encodedRows ...string) []byte {
	t.Helper()

	out := []byte(aokiTestTitle + aokiTestHeading)
	for _, encoded := range encodedRows {
		row, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("decoding fixture row: %v", err)
		}
		out = append(out, row...)
		out = append(out, '\r', '\n')
	}
	return out
}
