// utils for creating filters and sorting parameters from query strings
package storage

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
)

type Order int

const (
	ASC           Order = +1
	DESC          Order = -1
	notSet        Order = 0
	fieldDotField       = `(\w+(?:\.\w+)*)`
)

func parse_sort_query(sortstr string) (bson.D, error) {
	// ?sort=+modifiedBy,-timestamp
	// ?sort=+modifiedBy&sort=-timestamp

	var validQuery = regexp.MustCompile(`^(\+|-)` + fieldDotField + `$`)

	// bson.D is required to keep the order, its a list of bson.E
	var result bson.D
	parts := strings.Split(sortstr, ",")
	for i, p := range parts {
		matches := validQuery.FindStringSubmatch(p)
		// the full match and the two subgroups should be returned in a valid query
		if len(matches) != 3 {
			return bson.D{}, fmt.Errorf("cannot parse sort (%d:%s) from query (%s)", i, p, sortstr)
		}
		order := notSet
		switch matches[1] {
		case "+":
			order = ASC
		case "-":
			order = DESC
		}

		if order == notSet {
			return bson.D{}, fmt.Errorf("can never parse sort (%d,%s) from query (%s)", i, p, sortstr)
		}

		result = append(result, bson.E{Key: matches[2], Value: int(order)})
	}

	return result, nil
}

func sort_queries(sortstrings []string) (bson.D, error) {
	sorts := make([]bson.E, 0, len(sortstrings))

	for _, f := range sortstrings {
		mf, err := parse_sort_query(f)
		if err != nil {
			return nil, errors.Wrap(err, "could not parse filter query")
		}
		sorts = append(sorts, mf...)
	}
	return sorts, nil
}

func mongo_operator(op string, value interface{}) (bson.D, error) {
	switch op {
	case "==":
		// exact match
		return bson.D{bson.E{Key: "$eq", Value: value}}, nil
	case "!=":
		return bson.D{bson.E{Key: "$neq", Value: value}}, nil
	case ">=":
		return bson.D{bson.E{Key: "$gte", Value: value}}, nil
	case "<=":
		return bson.D{bson.E{Key: "$lte", Value: value}}, nil
	case ">":
		return bson.D{bson.E{Key: "$gt", Value: value}}, nil
	case "<":
		return bson.D{bson.E{Key: "$lt", Value: value}}, nil
	case "search":
		// make regexp case insensitive
		return bson.D{bson.E{Key: "$regex", Value: value}, bson.E{Key: "$options", Value: "i"}}, nil
	default:
		return bson.D{}, fmt.Errorf("no such filter operator (%s)", op)
	}
}

func mongo_filter(attr string, ops string, value interface{}) (bson.E, error) {
	op, err := mongo_operator(ops, value)
	if err != nil {
		return bson.E{}, errors.Wrap(err, "could not construct filter")
	}
	return bson.E{Key: attr, Value: op}, nil
}

func filter_queries(filterstrings []string) ([]bson.D, error) {
	filters := make([]bson.D, 0, len(filterstrings))

	for _, f := range filterstrings {
		mf, err := parse_filter_query(f)
		if err != nil {
			return nil, errors.Wrap(err, "could not parse filter query")
		}
		filters = append(filters, mf)
	}
	return filters, nil
}

func parse_filter_query(filter string) (bson.D, error) {
	// LHS brackets from https://www.moesif.com/blog/technical/api-design/REST-API-Design-Filtering-Sorting-and-Pagination/
	// ?filter=modifiedBy[==]=flow@equinor.com

	var validFilter = regexp.MustCompile(`^` + fieldDotField + `\[(==|>=|<=|search|>|<|!=)\]=(.*)$`)

	parts := strings.Split(filter, ",")

	// bson.D is required to keep the order (its a list of bson.E)
	result := make([]bson.E, 0, len(parts))
	for i, p := range parts {
		matches := validFilter.FindStringSubmatch(p)
		// the full match and the three subgroups should be returned for a valid query
		if len(matches) != 4 {
			return bson.D{}, fmt.Errorf("cannot parse filter (%d:%s) from query (%s)", i, p, filter)
		}

		opName := matches[2]
		attributeName := matches[1]
		var value interface{}
		var err error
		switch attributeName {
		case "timestamp":
			value, err = time.Parse(time.RFC3339, matches[3])
			if err != nil {
				return bson.D{}, errors.Wrapf(err, "cannot parse timestamp (%s) in (%d:%s) from query (%s)", matches[3], i, p, filter)
			}

		default:
			value = matches[3]
		}

		filter, err := mongo_filter(attributeName, opName, value)
		if err != nil {
			return bson.D{}, errors.Wrapf(err, "cannot parse filter (%d:%s) from query (%s)", i, p, filter)
		}

		result = append(result, filter)
	}

	return result, nil
}

type JoinOp string

const (
	AND JoinOp = "$and"
)

// joins a list of (filter) queries with the specified mongo operator. handles degenerate cases (singular or empty) gracefully
func join_queries(queries []bson.D, op JoinOp) bson.D {
	switch len(queries) {
	case 0:
		return bson.D{}
	case 1:
		return queries[0]
	}
	arr := make(bson.A, 0, len(queries))
	for _, q := range queries {
		arr = append(arr, q)
	}
	return bson.D{{Key: string(op), Value: arr}}
}
