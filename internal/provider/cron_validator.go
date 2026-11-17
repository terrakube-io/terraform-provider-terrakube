package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// Terrakube schedules a workspace with Quartz (`new CronExpression(cron)`), and it only parses
// the expression when the job is scheduled, not when the API stores it. A value Quartz rejects
// is therefore saved without an error and breaks the schedule later. validateQuartzCron checks
// the same grammar at plan time.
//
// It follows Quartz's CronExpression: 6 fields, or 7 with a year, and exactly one of
// day-of-month and day-of-week must be `?`. It is checked against Quartz 2.5.2, the version
// Terrakube ships, with the cases in testdata/quartz_cron.txt.

type cronField struct {
	name  string
	min   int
	max   int
	names map[string]int
}

var (
	cronMonthNames = map[string]int{
		"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4, "MAY": 5, "JUN": 6,
		"JUL": 7, "AUG": 8, "SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
	}
	cronWeekdayNames = map[string]int{
		"SUN": 1, "MON": 2, "TUE": 3, "WED": 4, "THU": 5, "FRI": 6, "SAT": 7,
	}
	cronFields = []cronField{
		{name: "seconds", min: 0, max: 59},
		{name: "minutes", min: 0, max: 59},
		{name: "hours", min: 0, max: 23},
		{name: "day of month", min: 1, max: 31},
		{name: "month", min: 1, max: 12, names: cronMonthNames},
		{name: "day of week", min: 1, max: 7, names: cronWeekdayNames},
		{name: "year", min: 0, max: 1<<31 - 1},
	}
)

const (
	cronHours      = 2
	cronDayOfMonth = 3
	cronMonth      = 4
	cronDayOfWeek  = 5
	cronYear       = 6
)

func validateQuartzCron(expression string) error {
	// Quartz splits on space and tab only (`new StringTokenizer(expression, " \t")`) and then calls
	// trim() on each token, which removes every character up to U+0020. So a line break between
	// fields joins them, while one at the end of a field, such as the newline a heredoc adds, is
	// dropped. A non-breaking space pasted from a web page is part of a field, and makes it invalid.
	fields := strings.FieldsFunc(strings.ToUpper(expression), func(r rune) bool {
		return r == ' ' || r == '\t'
	})
	for i, field := range fields {
		fields[i] = strings.TrimFunc(field, func(r rune) bool { return r <= ' ' })
	}
	if len(fields) < 6 || len(fields) > 7 {
		return fmt.Errorf("expected 6 or 7 fields (seconds minutes hours day-of-month month day-of-week [year]), got %d", len(fields))
	}

	for i, value := range fields {
		if err := validateCronField(i, cronFields[i], value); err != nil {
			return fmt.Errorf("%s: %w", cronFields[i].name, err)
		}
	}

	if isCronNoValue(fields[cronDayOfMonth]) == isCronNoValue(fields[cronDayOfWeek]) {
		return fmt.Errorf("exactly one of day of month and day of week must be ?, the other one a value")
	}
	return nil
}

// isCronNoValue reports whether a field is `?`, ignoring the empty list entries Quartz skips.
func isCronNoValue(value string) bool {
	trimmed := strings.Trim(value, ",")
	// Quartz also lets a single character follow the `?`.
	return trimmed == "?" || (len(trimmed) == 2 && trimmed[0] == '?')
}

func validateCronField(position int, field cronField, value string) error {
	if isCronNoValue(value) {
		if position != cronDayOfMonth && position != cronDayOfWeek {
			return fmt.Errorf("? is only allowed for day of month and day of week")
		}
		return nil
	}

	// Quartz does not support L in a list of days of the week, or more than one # (a second "nth"
	// day). A single n#m can share a list with other days, such as 2#1,3.
	if position == cronDayOfWeek {
		if strings.Contains(value, ",") && strings.Contains(value, "L") {
			return fmt.Errorf("the L form cannot be combined with other days in a list")
		}
		if strings.Count(value, "#") > 1 {
			return fmt.Errorf("only one # form is allowed in the day of week")
		}
	}

	// Quartz skips empty list entries, so a trailing comma is accepted.
	for _, element := range strings.Split(value, ",") {
		if element == "" {
			continue
		}
		if err := validateCronElement(position, field, element); err != nil {
			return err
		}
	}
	return nil
}

// cronWildcard is the value Quartz uses internally for `*`. It accepts it as a number too.
const cronWildcard = 99

// validateCronElement checks one entry of a list. Like Quartz, it reads the leading number or
// name of a value and ignores what follows it, so only a malformed start is an error.
func validateCronElement(position int, field cronField, element string) error {
	if strings.Count(element, "/") > 1 {
		return fmt.Errorf("%q: only one / is allowed", element)
	}

	// L and W belong to day of month and day of week, and # to day of week.
	if position <= cronHours && strings.ContainsAny(element, "LW#") {
		return fmt.Errorf("%q: L, W and # are not allowed in this field", element)
	}
	if position == cronDayOfMonth && strings.Contains(element, "#") {
		return fmt.Errorf("%q: # is only allowed in day of week", element)
	}

	switch position {
	case cronDayOfMonth:
		if ok, err := validateDayOfMonthSpecial(element); ok {
			return err
		}
	case cronDayOfWeek:
		if ok, err := validateDayOfWeekSpecial(element); ok {
			return err
		}
	}
	// Quartz also accepts L, LW and L-n in the month, day of week and year fields, and nothing
	// else with L, W or # in month and year.
	if position >= cronMonth && isCronLastForm(element) {
		return nil
	}
	if (position == cronMonth || position == cronYear) && strings.ContainsAny(element, "LW#") && !startsWithCronName(field, element) {
		return fmt.Errorf("%q: L, W and # are not allowed in this field", element)
	}

	base, step, hasStep := strings.Cut(element, "/")
	wildcard := base == "" || strings.HasPrefix(base, "*")

	if hasStep {
		n, ok := leadingNumber(step)
		if !ok {
			return fmt.Errorf("%q: the step after / must be a number", element)
		}
		// Quartz limits the step for `*/n` and `value/n`, but not for `from-to/n`.
		if !strings.Contains(base, "-") && field.name != "year" && n > field.max {
			return fmt.Errorf("%q: the step %d is larger than %d", element, n, field.max)
		}
	}
	if wildcard {
		return nil
	}

	start, rest, isRange := strings.Cut(base, "-")
	startValue, err := parseCronValue(field, start)
	if err != nil {
		return fmt.Errorf("%q: %w", element, err)
	}
	if !isRange {
		return nil
	}
	endValue, err := parseCronValue(field, rest)
	if err == nil && endValue == cronWildcard && !isInCronRange(field, endValue) {
		// 99 stands for `*` as a single value or the start of a range, but not as the end.
		err = fmt.Errorf("%d is outside %d-%d", endValue, field.min, field.max)
	}
	if err != nil {
		return fmt.Errorf("%q: %w", element, err)
	}
	if field.name == "year" && endValue < startValue {
		return fmt.Errorf("%q: the start year must not be after the end year", element)
	}
	return nil
}

// validateDayOfMonthSpecial handles L, LW, L-n and nW. The first result reports whether the
// element is one of them.
func validateDayOfMonthSpecial(element string) (bool, error) {
	switch {
	case element == "L" || element == "LW":
		return true, nil
	case strings.HasPrefix(element, "L-"):
		n, ok := leadingNumber(strings.TrimPrefix(element, "L-"))
		if !ok || n > 30 {
			return true, fmt.Errorf("%q: the offset after L- must be between 0 and 30", element)
		}
		return true, nil
	case strings.HasSuffix(element, "W"):
		n, ok := leadingNumber(strings.TrimSuffix(element, "W"))
		if !ok || n > 31 {
			return true, fmt.Errorf("%q: the day before W must be between 1 and 31", element)
		}
		return true, nil
	case strings.HasSuffix(element, "L"):
		return true, fmt.Errorf("%q: L can only stand alone or as L-n in the day of month", element)
	}
	return false, nil
}

// validateDayOfWeekSpecial handles L, nL and n#m, and rejects nW, which only day of month has.
func validateDayOfWeekSpecial(element string) (bool, error) {
	field := cronFields[cronDayOfWeek]
	switch {
	case strings.HasSuffix(element, "W") && element[0] >= '0' && element[0] <= '9':
		return true, fmt.Errorf("%q: W is only allowed in day of month", element)
	case element == "L":
		return true, nil
	case strings.Contains(element, "#"):
		day, nth, _ := strings.Cut(element, "#")
		if _, err := parseCronValue(field, day); err != nil {
			return true, fmt.Errorf("%q: %w", element, err)
		}
		n, ok := leadingNumber(nth)
		if !ok || n < 1 || n > 5 {
			return true, fmt.Errorf("%q: the number after # must be between 1 and 5", element)
		}
		return true, nil
	case strings.HasSuffix(element, "L"):
		if _, err := parseCronValue(field, strings.TrimSuffix(element, "L")); err != nil {
			return true, fmt.Errorf("%q: %w", element, err)
		}
		return true, nil
	}
	return false, nil
}

func startsWithCronName(field cronField, element string) bool {
	if len(element) < 3 {
		return false
	}
	_, ok := field.names[element[:3]]
	return ok
}

func isCronLastForm(element string) bool {
	if element == "L" || element == "LW" {
		return true
	}
	if rest, ok := strings.CutPrefix(element, "L-"); ok {
		_, ok := leadingNumber(rest)
		return ok
	}
	return false
}

// leadingNumber returns the digits at the start of s.
func leadingNumber(s string) (int, bool) {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 || end > 9 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:end])
	return n, err == nil
}

func isInCronRange(field cronField, n int) bool {
	return n >= field.min && (field.name == "year" || n <= field.max)
}

// parseCronValue reads a number or, for month and day of week, a name from the start of s.
func parseCronValue(field cronField, s string) (int, error) {
	if len(s) >= 3 {
		if n, ok := field.names[s[:3]]; ok {
			return n, nil
		}
	}
	n, ok := leadingNumber(s)
	if !ok {
		return 0, fmt.Errorf("%q is not a valid %s value", s, field.name)
	}
	if n == cronWildcard {
		return n, nil
	}
	if n < field.min || (field.name != "year" && n > field.max) {
		return 0, fmt.Errorf("%d is outside %d-%d", n, field.min, field.max)
	}
	return n, nil
}

// quartzCronValidator is a schema validator for a Quartz cron expression.
type quartzCronValidator struct{}

func (quartzCronValidator) Description(context.Context) string {
	return "value must be a valid Quartz cron expression, for example `0 0 2 * * ?`"
}

func (v quartzCronValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (quartzCronValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	// Unknown values are validated again once they are known.
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if err := validateQuartzCron(req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid cron expression",
			fmt.Sprintf("%q is not a valid Quartz cron expression: %s. Terrakube uses Quartz, so it needs 6 or 7 fields, for example \"0 0 2 * * ?\".", req.ConfigValue.ValueString(), err))
	}
}
