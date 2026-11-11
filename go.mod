module portexa

go 1.26.0

require (
	github.com/shopspring/decimal v1.6.0
	golang.org/x/crypto v0.58.0
	golang.org/x/sys v0.50.0
	golang.org/x/term v0.48.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
	modernc.org/sqlite v1.62.0
	portexa/mt4 v0.0.0
)

require (
	github.com/anchore/go-lzo v0.1.2-0.20261030041440-c6cb39a30b97 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	github.com/ncruces/go-strftime v1.1.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/text v0.43.0 // indirect
	modernc.org/libc v1.80.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

replace portexa/mt4 => ./packages/mt4
