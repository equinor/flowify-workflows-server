package storage

import (
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
)

func init() {

}

func Test_SortParse(t *testing.T) {
	makeErr := func(query string, no int, part string) error {
		// just copied from the internal function
		return fmt.Errorf("cannot parse sort (%d:%s) from query (%s)", no, part, query)
	}
	emptyErr := makeErr("", 0, "")

	var testCases = []struct {
		Name           string // name
		Query          string // input
		ExpectedError  error  // expected error
		ExpectedResult bson.D // expected return
	}{
		{"Empty query", "", emptyErr, bson.D{}},
		{"Simple ascending", "+timestamp", nil, bson.D{bson.E{Key: "timestamp", Value: int(ASC)}}},
		{"No ascending", "timestamp", makeErr("timestamp", 0, "timestamp"), bson.D{}},
		{"Bad direction", ".timestamp", makeErr(".timestamp", 0, ".timestamp"), bson.D{}},
		{"Simple descending", "-timestamp", nil, bson.D{bson.E{Key: "timestamp", Value: int(DESC)}}},
		{"Double ascending", "+modifiedBy,+timestamp", nil, bson.D{bson.E{Key: "modifiedBy", Value: int(ASC)}, bson.E{Key: "timestamp", Value: int(ASC)}}},
		{"Trailing comma", "+modifiedBy,", makeErr("+modifiedBy,", 1, ""), bson.D{}},
		{"Double error", "+modifiedBy+timestamp", makeErr("+modifiedBy+timestamp", 0, "+modifiedBy+timestamp"), bson.D{}},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			logrus.Info("Test: ", test.Name, ": ", test.Query)

			res, err := parse_sort_query(test.Query)

			assert.Equal(t, test.ExpectedResult, res)
			assert.Equal(t, test.ExpectedError, err)
		})
	}
}

func Test_FilterParse(t *testing.T) {
	makeErr := func(query string, no int, part string) error {
		// just copied from the internal function
		return fmt.Errorf("cannot parse filter (%d:%s) from query (%s)", no, part, query)
	}
	emptyErr := makeErr("", 0, "")
	isoNow := time.Now().UTC() //.Truncate(time.Second) // RFC3339 has no subsecond precision

	var testCases = []struct {
		Name           string // name
		Query          string // input
		ExpectedError  error  // expected error
		ExpectedResult bson.D // expected return
	}{
		{"Empty filter", "", emptyErr, bson.D{}},
		{"Filter on exact modifiedBy", "modifiedBy[==]=flow@equinor.com", nil,
			bson.D{{Key: "modifiedBy", Value: bson.D{{Key: "$eq", Value: "flow@equinor.com"}}}}},
		{"Filter on regexp modifiedBy", `modifiedBy[search]=\w@equinor.com`, nil,
			bson.D{bson.E{Key: "modifiedBy", Value: bson.D{{Key: "$regex", Value: `\w@equinor.com`}, {Key: "$options", Value: "i"}}}}},
		{"Timestamp", fmt.Sprintf("timestamp[<=]=%s", isoNow.Format(time.RFC3339Nano)), nil, bson.D{bson.E{Key: "timestamp", Value: bson.D{{Key: "$lte", Value: isoNow}}}}},
		{"Combined", fmt.Sprintf("timestamp[<=]=%s,modifiedBy[==]=flow@equinor.com", isoNow.Format(time.RFC3339Nano)), nil,
			bson.D{
				{Key: "timestamp", Value: bson.D{{Key: "$lte", Value: isoNow}}},
				{Key: "modifiedBy", Value: bson.D{{Key: "$eq", Value: "flow@equinor.com"}}},
			}},
	}

	for _, test := range testCases {
		t.Run(test.Name, func(t *testing.T) {
			logrus.Info("Test: ", test.Name, ": ", test.Query)

			res, err := parse_filter_query(test.Query)

			assert.Equal(t, test.ExpectedResult, res)
			assert.Equal(t, test.ExpectedError, err)
		})
	}
}
