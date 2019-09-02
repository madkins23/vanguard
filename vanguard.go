package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/xerrors"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	gSht "github.com/madkins23/go-google/drive/sheets"
	gAuth "github.com/madkins23/go-google/oauth2"
	"github.com/madkins23/go-utils/path"
)

const (
	// Data load state.
	start = iota
	investments
	funds
)

const (
	// Column names.
	colAccount = "account"
	colActual  = "actual"
	colCurrent = "current"
	colPctOver = "pctover"
	colPct90   = "pct90"
	colPct95   = "pct95"
	colPct975  = "pct975"
	colPrice   = "price"
	colOver    = "over"
	colSymbol  = "symbol"
	colTarget  = "target"
	colTgtAmt  = "tgtamt"
)

const (
	// Help strings for command line flags.
	dataFlagHelp   = "path of data file [~/Downloads/ofxdownload.csv]"
	debugFlagHelp  = "debug level [1]"
	deleteFlagHelp = "delete data file after successful insertion [true]"
	helpFlagHelp   = "show usage data [true]"
	idFlagHelp     = "sheet ID (required)"
)

const (
	// Format strings.
	flagFormat    = "  %-16s  %s\n"
	accountFormat = "> %-12s %-32s %s"
	rowFormat     = ">        # %3d %8s %v%s"
)

type positionData struct {
	share string
	total string
}

var (
	// Command line flags.
	dataFlag   = flag.String("data", "~/Downloads/ofxdownload.csv", dataFlagHelp)
	debugFlag  = flag.Int("debug", 1, debugFlagHelp)
	deleteFlag = flag.Bool("delete", true, deleteFlagHelp)
	helpFlag   = flag.Bool("help", false, helpFlagHelp)
	idFlag     = flag.String("id", "", idFlagHelp)
)

func main() {
	fmt.Println("Vanguard starting")

	flag.Parse()

	if *idFlag == "" {
		var found bool
		if *idFlag, found = os.LookupEnv("VANGUARD_ID"); !found {
			fmt.Println("**** --id=<sheetID> is required")
			usage()
			return
		}
	}

	if *helpFlag {
		usage()
		return
	}

	var err error

	dataPath := *dataFlag
	if strings.HasPrefix(dataPath, "~/") {
		dataPath, err = path.HomePath(dataPath[2:])
		if err != nil {
			fmt.Printf("*** Error fixing data path:\n%v\n", err)
			return
		}
	}
	if _, err = os.Stat(dataPath); err != nil {
		fmt.Printf("*** Data path %s error:\n%v\n", dataPath, err)
		return
	}

	data, err := loadData(dataPath)
	if err != nil {
		fmt.Printf("*** Error loading data:\n%v\n", err)
	}

	err = updateSpreadsheet(data)
	if err != nil {
		fmt.Printf("*** Error updating spreadsheet:\n%v\n", err)
	}

	if *deleteFlag {
		fmt.Printf("> Deleting %s\n", dataPath)
		if err := os.RemoveAll(dataPath); err != nil {
			fmt.Printf("!!! Error deleting %s:\n%v\n", dataPath, err)
		}
	}

	fmt.Println("Vanguard finished")
}

// loadData acquires downloaded data from the specified path.
func loadData(dataPath string) (map[string]map[string]*positionData, error) {
	fmt.Println("Load Data starting")

	file, err := os.Open(dataPath)
	if err != nil {
		return nil, xerrors.Errorf("open data file %s: %w", dataPath, err)
	}

	state := start
	positions := make(map[string]map[string]*positionData)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")

		if fields[1] == "Trade Date" {
			debugFmt(1, "- Skipping Trade Table\n")
			state = start
			continue
		}

		if fields[0] == "Fund Account Number" {
			if strings.Contains(fields[1], "Name") {
				debugFmt(1, "> Funds Table\n")
				state = funds
			} else {
				state = start
			}

			continue
		}

		if fields[0] == "Account Number" {
			if strings.Contains(fields[1], "Name") {
				debugFmt(1, "> Investments Table\n")
				state = investments
			} else {
				state = start
			}

			continue
		}

		if state == start {
			continue
		}

		var account = fields[0]
		var symbol = fields[2]
		var data = &positionData{}

		switch state {
		case funds:
			account = strings.Split(fields[0], "-")[1]
			symbol = symbolForFundName(fields[1])

			if symbol == "" {
				fmt.Println("! No symbol for fund name", fields[1])
				continue
			}

			data.share = fields[2]
			data.total = fields[4]
		case investments:
			if symbol == "" {
				if fields[1] == "CASH" {
					symbol = "CASH"
				} else {
					fmt.Println("! No symbol for investment name", fields[1])
					continue
				}
			}

			data.share = fields[4]
			data.total = fields[5]
		default:
			return nil, xerrors.Errorf("improper state %v", state)
		}

		if positions[account] == nil {
			positions[account] = make(map[string]*positionData)
		}

		positions[account][symbol] = data
	}

	if err := scanner.Err(); err != nil {
		return nil, xerrors.Errorf("reading data: %w", err)
	}

	if *debugFlag > 1 {
		for acct := range positions {
			_ = fmt.Sprintf("# %s\n", acct)
		}
	}

	fmt.Println("Load Data finished")

	return positions, nil
}

// updateSpreadsheet pushes loaded data into the user's spreadsheet.
func updateSpreadsheet(positions map[string]map[string]*positionData) error {
	fmt.Println("Update Spreadsheet starting")

	authorizer, err := gAuth.NewAuthorizer("vanguard", []string{"drive", "sheets"})
	if err != nil {
		return xerrors.Errorf("get authorizer: %w", err)
	}

	client, err := authorizer.GetClient()
	if err != nil {
		return xerrors.Errorf("acquire client: %w", err)
	}

	service, err := sheets.NewService(context.Background(), option.WithHTTPClient(client))
	if err != nil {
		return xerrors.Errorf("retrieve sheets servce: %w", err)
	}

	err = gSht.Limiter.Wait(context.Background())
	if err != nil {
		return xerrors.Errorf("limiter wait: %w", err)
	}

	spreadsheet, err := service.Spreadsheets.Get(*idFlag).Do()
	if err != nil {
		return xerrors.Errorf("retrieve spreadsheet: %w", err)
	}

	for _, sheet := range spreadsheet.Sheets {
		var account string
		title := sheet.Properties.Title

		debugFmt(2, accountFormat, "", title, "\r")

		// Parse headers from sheet:
		headers, err := service.Spreadsheets.Values.Get(*idFlag, title+"!A1:Z2").Do()
		if err != nil {
			debugFmt(2, accountFormat, "", title, "no headers\n")
			return xerrors.Errorf("retrieve header row: %w", err)
		}

		colA1 := make(map[string]string)
		column := make(map[string]int)
		for index, value := range headers.Values[0] {
			header, ok := value.(string)
			if !ok {
				debugFmt(2, accountFormat, "", title,
					fmt.Sprintf("column header %v not string\n", value))
				continue
			}
			header = strings.ToLower(header)

			if header == colAccount {
				// Capture account number for sheet from eponymously named column.
				account, ok = headers.Values[1][index].(string)
				if !ok {
					debugFmt(2, accountFormat, "", title,
						fmt.Sprintf("account %v not string\n", headers.Values[1][index]))
					continue
				}
			} else {
				// Column lookup points to string with column character.
				colA1[header] = gSht.ColToAlpha(index)
				column[header] = index
			}
		}

		if account == "" {
			debugFmt(2, accountFormat, "", title, "no account number\n")
			continue
		}

		accountPositions, found := positions[account]
		if !found || accountPositions == nil {
			debugFmt(2, accountFormat, "", title, "no account data in file\n")
			continue
		}

		debugFmt(1, accountFormat, account, title, "\r")

		// Get rows of data from sheet.
		grid := sheet.Properties.GridProperties
		if grid == nil {
			debugFmt(1, accountFormat, account, title, fmt.Sprintf("no grid for %v\n", sheet))
			continue
		}

		// Ask for size of grid, but grid is usually larger than non-empty section.
		// Therefore number of rows and columns returned is less than grid size.
		rows, err := service.Spreadsheets.Values.Get(*idFlag,
			title+"!A3:"+gSht.ColToAlpha(int(grid.ColumnCount))+strconv.Itoa(int(grid.RowCount-2))).Do()
		if err != nil {
			debugFmt(1, accountFormat, account, title, "no row data\n")
			return xerrors.Errorf("retrieve rows: %w", err)
		}

		rowNum := 2
		colSymbol := column[colSymbol]
		debugFmt(3, "")
		for _, row := range rows.Values {
			rowNum++
			debugFmt(3, rowFormat, rowNum, "", row, "\r")

			if colSymbol >= len(row) {
				continue
			}

			symbol, ok := row[colSymbol].(string)
			if !ok || symbol == "" {
				continue
			}

			data := accountPositions[symbol]
			if data == nil {
				data = &positionData{}
			} else {
				delete(accountPositions, symbol)
			}

			debugFmt(3, rowFormat, rowNum, symbol, data, "\n")

			err = gSht.Limiter.Wait(context.Background())
			if err != nil {
				return xerrors.Errorf("limiter wait: %w", err)
			}

			_, err := service.Spreadsheets.Values.BatchUpdate(*idFlag, &sheets.BatchUpdateValuesRequest{
				ValueInputOption: "USER_ENTERED",
				Data: []*sheets.ValueRange{
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colActual]),
						Values: [][]interface{}{{
							fmt.Sprintf("=%s%d/%s$2", colA1[colCurrent], rowNum, colA1[colCurrent]),
						}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colCurrent]),
						Values:         [][]interface{}{{data.total}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colOver]),
						Values: [][]interface{}{{
							fmt.Sprintf("=%s%d-%s%d", colA1[colCurrent], rowNum, colA1[colTgtAmt], rowNum),
						}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colPctOver]),
						Values: [][]interface{}{{
							fmt.Sprintf("=IF(%s%d>0,%s%d/%s%d,0)",
								colA1[colTgtAmt], rowNum, colA1[colOver], rowNum, colA1[colTgtAmt], rowNum),
						}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colPct90]),
						Values:         [][]interface{}{{fmt.Sprintf("=0.9*%s%d", colA1[colPrice], rowNum)}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colPct95]),
						Values:         [][]interface{}{{fmt.Sprintf("=0.95*%s%d", colA1[colPrice], rowNum)}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colPct975]),
						Values:         [][]interface{}{{fmt.Sprintf("=0.975*%s%d", colA1[colPrice], rowNum)}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colPrice]),
						Values:         [][]interface{}{{data.share}},
					},
					{
						MajorDimension: "ROWS",
						Range:          gSht.SheetRowCol(title, rowNum, column[colTgtAmt]),
						Values: [][]interface{}{{
							fmt.Sprintf("=%s$2*%s%d", colA1[colCurrent], colA1[colTarget], rowNum),
						}},
					},
				},
			}).Do()

			if err != nil {
				debugFmt(3, rowFormat, rowNum, symbol, "update error\n")
				return xerrors.Errorf("updating row: %w", err)
			}
		}

		debugFmt(1, accountFormat, account, title, "done\n")

		if len(accountPositions) > 0 {
			fmt.Println("!!! Unused symbols (must be added manually):")
			for symbol := range accountPositions {
				fmt.Printf("!!!   %s\n", symbol)
			}
		}
	}

	fmt.Println("Update Spreadsheet finished")

	return nil
}

func debugFmt(level int, format string, stuff ...interface{}) {
	if *debugFlag >= level {
		fmt.Printf(format, stuff...)

		if len(format) > 0 && format[len(format)-1:] == "\r" {
			_ = os.Stdout.Sync()
		} else if len(format) > 1 && len(stuff) > 0 && format[len(format)-2:] == "%s" {
			last := stuff[len(stuff)-1]
			if lastString, ok := last.(string); ok && lastString[len(lastString)-1:] == "\r" {
				_ = os.Stdout.Sync()
			}
		}
	}
}

func symbolForFundName(fundName string) string {
	switch fundName {
	case "Vanguard Balanced Index Fund Investor Shares":
		return "VBINX"
	case "Vanguard High Dividend Yield Index Fund Investor Shares":
		return "VHDYX"
	case "Vanguard Inflation-Protected Securities Fund Investor Shares":
		return "VIPSX"
	}

	return ""
}

func usage() {
	fmt.Println("Usage:  vanguard")
	fmt.Println("Flags:")
	fmt.Printf(flagFormat, "id", idFlagHelp)
	fmt.Printf(flagFormat, "delete", deleteFlagHelp)
	fmt.Printf(flagFormat, "debug", debugFlagHelp)
	fmt.Printf(flagFormat, "help", helpFlagHelp)
}
