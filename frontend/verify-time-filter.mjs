// Time-filter verification.
//
// The filtering logic lives inline in App.svelte, so rather than duplicating it
// here (which would let the two drift apart) these tests lift the actual
// functions out of the component source and exercise them. Run with:
//
//	node frontend/verify-time-filter.mjs
//
// It checks the rules that are easy to get wrong subtly: the 2400 end-of-day
// marker, ranges spanning midnight, full-day entries, "soon" look-ahead, and
// malformed times not aborting the whole filter.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "src", "App.svelte"), "utf8");

// Pull out a function body by its opening line, matching to the line "  }" that
// closes an indented function inside the component script.
function extractFunction(signature) {
  const start = source.indexOf(signature);
  if (start < 0) throw new Error(`could not find ${signature} in App.svelte`);
  const end = source.indexOf("\n  }\n", start);
  if (end < 0) throw new Error(`could not find the end of ${signature}`);
  return source.slice(start, end + 4);
}

const timeToMinutesSrc = extractFunction("function timeToMinutes(timeString) {");
const isOnAirSrc = extractFunction(
  "function isOnAir(startTime, endTime, filterMinutes) {",
);

// Build the functions from the extracted source. `new Function` keeps this
// dependency-free and guarantees we are testing what ships. Both are compiled in
// one scope because isOnAir() calls timeToMinutes().
const { timeToMinutes, isOnAir } = new Function(
  `${timeToMinutesSrc}\n${isOnAirSrc}\nreturn { timeToMinutes, isOnAir };`,
)();

let failures = 0;
function check(label, got, want) {
  if (got !== want) {
    console.log(`  FAIL ${label}: got ${JSON.stringify(got)}, want ${JSON.stringify(want)}`);
    failures++;
  }
}

console.log("timeToMinutes");
const parse = [
  ["0000", 0],
  ["0001", 1],
  ["1230", 750],
  ["2359", 1439],
  ["2400", 0], // end of day == start of the next
  [" 1200 ", 720],
  ["", null],
  ["000", null],
  ["00000", null],
  ["0.", null],
  ["2401", null],
  ["2460", null],
  ["1260", null],
  ["abcd", null],
  [null, null],
  [undefined, null],
];
for (const [input, want] of parse) {
  check(`timeToMinutes(${JSON.stringify(input)})`, timeToMinutes(input), want);
}

console.log("isOnAir");
// Simple daytime window.
check("0900-1700 @0859", isOnAir("0900", "1700", 8 * 60 + 59), false);
check("0900-1700 @0900", isOnAir("0900", "1700", 9 * 60), true);
check("0900-1700 @1200", isOnAir("0900", "1700", 12 * 60), true);
check("0900-1700 @1700", isOnAir("0900", "1700", 17 * 60), true);
check("0900-1700 @1701", isOnAir("0900", "1700", 17 * 60 + 1), false);

// Ending at 2400: still on air at 00:00, the regression that hid 595 rows.
check("2300-2400 @2259", isOnAir("2300", "2400", 22 * 60 + 59), false);
check("2300-2400 @2300", isOnAir("2300", "2400", 23 * 60), true);
check("2300-2400 @2359", isOnAir("2300", "2400", 23 * 60 + 59), true);
check("2300-2400 @0000", isOnAir("2300", "2400", 0), true);

// Full-day entries.
check("0000-2400 @0000", isOnAir("0000", "2400", 0), true);
check("0000-2400 @1200", isOnAir("0000", "2400", 12 * 60), true);
check("0000-2400 @2359", isOnAir("0000", "2400", 1439), true);
check("1200-1200 @0000 (start==end)", isOnAir("1200", "1200", 0), true);
check("1200-1200 @1200 (start==end)", isOnAir("1200", "1200", 12 * 60), true);

// Ranges spanning midnight.
check("2300-0100 @2259", isOnAir("2300", "0100", 22 * 60 + 59), false);
check("2300-0100 @2300", isOnAir("2300", "0100", 23 * 60), true);
check("2300-0100 @0000", isOnAir("2300", "0100", 0), true);
check("2300-0100 @0100", isOnAir("2300", "0100", 60), true);
check("2300-0100 @0101", isOnAir("2300", "0100", 61), false);
check("2300-0100 @1200", isOnAir("2300", "0100", 12 * 60), false);

// A malformed time is simply not on air; it must not throw.
check("000-2400 (malformed)", isOnAir("000", "2400", 12 * 60), false);
check("0.- (malformed)", isOnAir("0.", "", 12 * 60), false);

// Every minute of the day must be decidable without throwing.
for (let m = 0; m < 1440; m++) {
  isOnAir("0000", "2400", m);
  isOnAir("2300", "0100", m);
}

console.log(
  failures === 0
    ? "\nRESULT: ALL TIME-FILTER CHECKS PASSED"
    : `\nRESULT: ${failures} FAILURE(S)`,
);
process.exit(failures === 0 ? 0 : 1);
