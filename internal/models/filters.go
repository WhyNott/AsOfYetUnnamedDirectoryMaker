package models

import (
	_ "encoding/json"
)

// There are used when selecting and validating moderator privileges

type ColumnIDType string

const (
	ColumnIDSingle ColumnIDType = "single"
	ColumnIDRange  ColumnIDType = "range"
)

type RangeFilterType string

const (
	RangeFilterAbove   RangeFilterType = "above"
	RangeFilterBelow   RangeFilterType = "below"
	RangeFilterBetween RangeFilterType = "between"
)

type FilterType string

const (
	FilterNumericRange FilterType = "numeric_range"
	FilterLocations    FilterType = "locations"
	FilterCategories   FilterType = "categories"
	FilterTags         FilterType = "tags"
)

// NumericRangeID is a unique identifier for numeric ranges
type NumericRangeID int

// ColumnID represents a column identifier
type ColumnID struct {
	Type  ColumnIDType `json:"type"`
	Value string       `json:"value,omitempty"` // for Single
	Start string       `json:"start,omitempty"` // for Range
	End   string       `json:"end,omitempty"`   // for Range
}

// Constructor functions for type safety
func NewSingleColumnID(value string) ColumnID {
	return ColumnID{
		Type:  ColumnIDSingle,
		Value: value,
	}
}

func NewRangeColumnID(start, end string) ColumnID {
	return ColumnID{
		Type:  ColumnIDRange,
		Start: start,
		End:   end,
	}
}

// RangeFilter represents different types of numeric ranges
type RangeFilter struct {
	Type      RangeFilterType `json:"type"`
	Threshold float64         `json:"threshold,omitempty"` // for Above/Below
	Min       float64         `json:"min,omitempty"`       // for Between
	Max       float64         `json:"max,omitempty"`       // for Between
}

// Constructor functions for type safety
func NewAboveRangeFilter(threshold float64) RangeFilter {
	return RangeFilter{
		Type:      RangeFilterAbove,
		Threshold: threshold,
	}
}

func NewBelowRangeFilter(threshold float64) RangeFilter {
	return RangeFilter{
		Type:      RangeFilterBelow,
		Threshold: threshold,
	}
}

func NewBetweenRangeFilter(min, max float64) RangeFilter {
	return RangeFilter{
		Type: RangeFilterBetween,
		Min:  min,
		Max:  max,
	}
}

// Filter represents different types of filters
type Filter struct {
	Type   FilterType     `json:"type"`
	Range  *RangeFilter   `json:"range,omitempty"`  // for NumericRange
	ID     NumericRangeID `json:"id,omitempty"`     // for NumericRange
	Values []string       `json:"values,omitempty"` // for Locations/Categories/Tags
}

// Constructor functions for type safety
func NewNumericRangeFilter(rangeFilter RangeFilter, id NumericRangeID) Filter {
	return Filter{
		Type:  FilterNumericRange,
		Range: &rangeFilter,
		ID:    id,
	}
}

func NewLocationsFilter(values []string) Filter {
	return Filter{
		Type:   FilterLocations,
		Values: values,
	}
}

func NewCategoriesFilter(values []string) Filter {
	return Filter{
		Type:   FilterCategories,
		Values: values,
	}
}

func NewTagsFilter(values []string) Filter {
	return Filter{
		Type:   FilterTags,
		Values: values,
	}
}

// Control represents a column-filter pair
type Control struct {
	Column ColumnID `json:"column"`
	Filter Filter   `json:"filter"`
}

// Controls is a slice of Control structs
type Controls []Control

func (c ColumnID) IsValid() bool {
	switch c.Type {
	case ColumnIDSingle:
		return c.Value != ""
	case ColumnIDRange:
		return c.Start != "" && c.End != ""
	default:
		return false
	}
}

func (r RangeFilter) IsValid() bool {
	switch r.Type {
	case RangeFilterAbove, RangeFilterBelow:
		return r.Threshold != 0
	case RangeFilterBetween:
		return r.Min < r.Max
	default:
		return false
	}
}

func (f Filter) IsValid() bool {
	switch f.Type {
	case FilterNumericRange:
		return f.Range != nil && f.Range.IsValid()
	case FilterLocations, FilterCategories, FilterTags:
		return len(f.Values) > 0
	default:
		return false
	}
}
