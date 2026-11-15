package main

// eibiTestReadme is a trimmed but structurally faithful copy of EiBi's
// README.TXT: section headings are indented, countries carry an ITU code, and
// transmitter sites appear both on the country line and on continuation lines.
const eibiTestReadme = `How to use EiBi frequency lists.
================================

C) Available formats.
D) Codes used.

================================

   I) Language codes.

   Numbers are number of speakers. 4m = 4 millions.
   On the right in [brackets] the ISO 639-3 (SIL) language code for reference.

   -CW   Morse Station
   -TS   Time Signal Station
   A     Arabic (300m)                                                       [arb]
   AB    Abkhaz: Georgia-Abkhazia (0.1m)                                     [abk]
   BIS   Bislama (200,000)                                                   [bis]
   ENG   English (400m)                                                      [eng]
   ZWE   Languages of Zimbabwe
         Zomi-Chin: see Chin-Zomi (C-Z)

   II) Country codes.
   Countries are referred to by their ITU code
   Asterisks (*) denote non-official abbreviations

   AFG  Afghanistan
   AFS  South Africa
   B    Brasil
   HOL  The Netherlands
   OMA  Oman
   TWN  Taiwan *
   USA  United States of America

   III) Target-area codes.
   Af  - Africa
   Am  - America(s)
   Eu  - Europe (often including North Africa/Middle East)
   FE  - Far East
   Glo - Global
   N.. - North ..
   NAO - North Atlantic Ocean
   SEA - South East Asia

   Alternatively, ITU country codes may be used if the target area is limited to
   a certain country.

   IV) Transmitter site codes.
   One-letter or two-letter codes for different transmitter sites within one country.

   The code is used plainly if the transmitter is located within the home country of the station.

   AFG: k-Kabul / Pol-e-Charkhi 34N32-69E20
        x-Khost 33N14-69E49
   AFS: Meyerton 26S35-28E08 except:
        ct-Cape Town 33S41-18E42
        j-Johannesburg 26S07'40"-28E12'20"
   AGL: L-Luena (Moxico) 11S47'00"-19E55'19"
        lu-Luanda Radio 08S48-13E16
   ALB: c-Cerrik (CRI) 41N00-20E00
        f-Fllake (Durres, 500kW) 41N22-19E30
   TWN: f-Fangliao FAN 22N23-120E34
        L-Lukang 24N03-120E25
   USA: a-Andrews AFB, MD 38N48'39"-76W52'01"
        b-Birmingham / Vandiver, AL (WEWN) 33N30'13"-86W28'27"

US Air Force Messages: Sites are ALS-e, ASC, AZR-lj
`

// eibiTestCsv is the header plus a few rows modelled on sked-b26.csv.
const eibiTestCsv = "kHz:75;Time(UTC):93;Days:59;ITU:49;Station:201;Lng:49;Target:62;Remarks:135;P:35;Start:60;Stop:60;\r\n" +
	"3265;0000-2400;;USA;WBCQ Monticello;Eng;NAm;a;1;;\r\n" +
	"6005;0000-2400;;HOL;Radio Netherlands;Nld;WEu;ANK;0;;\r\n" +
	"9585;1100-1200;Fr-Su;OMA;BBC;A,Dar;AFG;/AFG-k;1;;[0212]\r\n" +
	"9720;0000-2400;;TWN;Sound of Hope;M;FE;;1;;\r\n"
