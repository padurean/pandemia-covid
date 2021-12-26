package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"time"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

func init() {
	http.DefaultClient.Timeout = time.Second * 15
}

// CountryCode ...
type CountryCode string

// CountryName ...
type CountryName string

// CountryData ...
type CountryData struct {
	Data []DayData `json:"data"`
}

// DayData ...
type DayData struct {
	Date                string  `json:"date"`
	NewDeathsPerMillion float32 `json:"new_deaths_per_million"`
}

var countries = map[CountryCode]CountryName{
	"ROU": "România",
	// "NLD": "Olanda",
	"DEU": "Germania",
	"ITA": "Italia",
	// "NOR": "Norvegia",
	"DNK": "Danemarca",
	// "SWE": "Suedia",
	// "ISR": "Israel",
}

func main() {
	data, err := getData(countries, 0)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := renderDailyDeathsPerMillionChart(data); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getData(countries map[CountryCode]CountryName, onlyLast int) (map[CountryCode]CountryData, error) {
	dataPath := "pkg/data"
	dataFileName := "owid-covid-data.json"
	dataFilePath := path.Join(dataPath, dataFileName)

	// check if data file exists and is not stale
	dataFileInfo, err := os.Stat(dataFilePath)
	dataFileDoesNotExist := errors.Is(err, os.ErrNotExist)
	if err != nil && !dataFileDoesNotExist {
		return nil, fmt.Errorf("error getting info for data file %s: %v", dataFilePath, err)
	}

	dataIsStale := dataFileInfo != nil && time.Since(dataFileInfo.ModTime()) > 24*time.Hour

	// check if data from file already includes all countries
	dataIncludesAllRequestedCountries := true
	if !dataFileDoesNotExist && !dataIsStale {
		data, err := readJSONData(dataFilePath)
		if err != nil {
			return nil, fmt.Errorf("error reading data from file to check if all countries are there: %v", err)
		}
		dataIncludesAllRequestedCountries = err == nil && len(data) >= len(countries)
		if dataIncludesAllRequestedCountries {
			for country := range countries {
				if _, ok := data[country]; !ok {
					dataIncludesAllRequestedCountries = false
					break
				}
			}
		}
	}

	// download data if local data does not exist, is stale or does not include all countries
	if dataFileDoesNotExist || dataIsStale || !dataIncludesAllRequestedCountries {
		dataDownloadURL := "https://covid.ourworldindata.org/data/" + dataFileName
		if err := downloadJSONData(dataDownloadURL, dataFilePath, countries); err != nil {
			return nil, err
		}
	}

	data, err := readJSONData(dataFilePath)
	if err != nil {
		return nil, err
	}

	dataForSpecifiedCountries := make(map[CountryCode]CountryData)
	for cc, cd := range data {
		if _, ok := countries[cc]; ok {
			dataForSpecifiedCountries[cc] = cd
		}
	}

	if onlyLast <= 0 {
		return dataForSpecifiedCountries, nil
	}

	onlyLastData := make(map[CountryCode]CountryData)
	for cc, cd := range dataForSpecifiedCountries {
		cd.Data = cd.Data[len(cd.Data)-onlyLast:]
		onlyLastData[cc] = cd
	}

	return onlyLastData, nil
}

func readJSONData(file string) (map[CountryCode]CountryData, error) {
	dataBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("error reading data file %s: %v", file, err)
	}

	var data map[CountryCode]CountryData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("error unmarshaling data from JSON file %s: %v", file, err)
	}

	return data, nil
}

func downloadJSONData(url, file string, countries map[CountryCode]CountryName) error {
	fmt.Printf("downloading data from URL %s to file %s ...\n", url, file)

	response, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("error downloading data from URL %s: %v", url, err)
	}

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("error reading data download response from URL %s: %v", url, err)
	}

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"error downloading data from URL %s: expected status %d %s, got %s with body %s",
			url, http.StatusOK, http.StatusText(http.StatusOK), response.Status, responseBody)
	}

	var allData map[CountryCode]CountryData
	if err := json.Unmarshal(responseBody, &allData); err != nil {
		return fmt.Errorf("error unmarshaling downloaded data from JSON response: %v", err)
	}

	// availableCountries := make([]Country, 0, len(allData))
	filteredData := make(map[CountryCode]CountryData)
	for country, data := range allData {
		if _, ok := countries[country]; ok {
			filteredData[country] = data
		}
		if len(filteredData) == len(countries) {
			break
		}
		// availableCountries = append(availableCountries, country)
	}
	// fmt.Println("available countries:", availableCountries)

	if len(filteredData) < len(countries) {
		var missingCountries []CountryCode
		for country := range countries {
			if _, ok := filteredData[country]; !ok {
				missingCountries = append(missingCountries, country)
			}
		}
		return fmt.Errorf(
			"downloaded data does not contain all the requested countries; missing: %v",
			missingCountries)
	}

	filteredDataBytes, err := json.MarshalIndent(filteredData, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling filtered data to JSON: %v", err)
	}

	if err := os.WriteFile(file, filteredDataBytes, 0644); err != nil {
		return fmt.Errorf("error writing downloaded data to file %s: %v", file, err)
	}

	return nil
}

func renderDailyDeathsPerMillionChart(data map[CountryCode]CountryData) error {
	fmt.Println("rendering daily deaths per million chart ...")

	chart := charts.NewLine()
	chart.
		SetGlobalOptions(
			charts.WithInitializationOpts(opts.Initialization{Theme: types.ThemeWesteros}),
			charts.WithTitleOpts(opts.Title{
				Title:    "Pandemia cu și fără Valuri",
				Subtitle: "Decese zilnice la 1 milion de locuitori",
			}),
			charts.WithLegendOpts(opts.Legend{Show: true}),
			charts.WithDataZoomOpts(opts.DataZoom{}),
			charts.WithTooltipOpts(opts.Tooltip{Show: true}),
		)

	days := make(map[string]int)
	for _, countryData := range data {
		for _, d := range countryData.Data {
			days[d.Date]++
		}
	}

	nbCountries := len(data)
	daysCommonToAllCountries := make([]string, 0, len(days))
	for day, counter := range days {
		if counter == nbCountries {
			daysCommonToAllCountries = append(daysCommonToAllCountries, day)
		}
	}
	sort.Strings(daysCommonToAllCountries)
	chart.SetXAxis(daysCommonToAllCountries)

	for countryCode, countryData := range data {
		linesData := make([]opts.LineData, 0, len(countryData.Data))
		for _, d := range countryData.Data {
			if days[d.Date] == nbCountries {
				linesData = append(linesData, opts.LineData{Value: d.NewDeathsPerMillion})
			}
		}
		chart.AddSeries(string(countries[countryCode]), linesData)
	}

	chart.SetSeriesOptions(
		charts.WithLineChartOpts(opts.LineChart{Smooth: false}),
		charts.WithMarkLineNameTypeItemOpts(opts.MarkLineNameTypeItem{
			Name: "Average",
			Type: "average",
		}),
		charts.WithMarkPointStyleOpts(opts.MarkPointStyle{
			Label: &opts.Label{
				Show:      true,
				Formatter: "{a}: {b}",
			},
		}),
		charts.WithAreaStyleOpts(opts.AreaStyle{
			Opacity: 0.2,
		}),
	)

	f, err := os.OpenFile("pkg/charts/deaths.html", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("error opening chart file for writing: %v", err)
	}
	defer f.Close()

	return chart.Render(f)
}
