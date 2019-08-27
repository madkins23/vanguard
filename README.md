# vanguard

A simple Go script to process Vanguard CSV downloads into a Google spreadsheet.

## Caveats

* This code is presented as an example, not a production-ready application.
* Don't expect support or excessive documentation.
* (At least) one aspect of the code is peculiar to Linux.
* Fork the repository or copy the code for your own use.

## Usage

1. Login to Vanguard and download the last month's data as a CSV.

1. Create a [Google spreadsheet](https://www.google.com/sheets/about/) for your accounts.

    1. Each tab in the spreadsheet should have the following columns, named in the first header row:
        * `Locality`
        * `Strategy`
        * `Focus`
        * `Weight`
        * `Specific`
        * `Symbol`
        * `Target`
        * `TgtAmt`
        * `Actual`
        * `Current`
        * `Over`
        * `PctOver`
        * `Price`
        * `Pct975`
        * `Pct95`
        * `Pct90`
        * `Account`

    1. On the second row enter vertical sums for:
        * `Target`
        * `TgtAmt`
        * `Actual`
        * `Current`

    1. On the second row enter the account number for the `Account` that shows in the current tab.

    1. Enter symbols for stocks under the `Symbol` column starting with the third row.
       New rows are not automatically added at this time, they are just logged.

1. The script currently expects file `~/Downloads/ofxdownload.csv` as input.
    * By default this file will be deleted after use unless `--delete=false` is set on the command line.
    * Configure the spreadsheet using its ID from the URL (e.g. `https://docs.google.com/spreadsheets/d/nastyLongIDHere`)
      using the `--id=nastyLongIDHere` flag.
