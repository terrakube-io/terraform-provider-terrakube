package provider

import (
	"bufio"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// testdata/quartz_cron.txt has one expression per line, prefixed with OK or ERR. The verdict is
// what Quartz 2.5.2, the version Terrakube ships, returns for CronExpression.isValidExpression.
// The expressions were generated: the Quartz documentation examples, common mistakes such as a
// five field Unix cron, and a seeded sample of valid and invalid combinations for every field.
func TestValidateQuartzCron_MatchesQuartz(t *testing.T) {
	file, err := os.Open("testdata/quartz_cron.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	cases := 0
	for scanner.Scan() {
		line := scanner.Text()
		var quartzAccepts bool
		var expression string
		switch {
		case strings.HasPrefix(line, "OK "):
			quartzAccepts, expression = true, line[len("OK "):]
		case strings.HasPrefix(line, "ERR "):
			quartzAccepts, expression = false, line[len("ERR "):]
		case line == "ERR":
			quartzAccepts, expression = false, ""
		default:
			t.Fatalf("unexpected line in testdata/quartz_cron.txt: %q", line)
		}
		cases++

		err := validateQuartzCron(expression)
		switch {
		case quartzAccepts && err != nil:
			t.Errorf("Quartz accepts %q but validateQuartzCron rejects it: %v", expression, err)
		case !quartzAccepts && err == nil:
			t.Errorf("Quartz rejects %q but validateQuartzCron accepts it", expression)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if cases < 1000 {
		t.Fatalf("expected the full Quartz sample, read only %d cases", cases)
	}
}

func TestValidateQuartzCron_Messages(t *testing.T) {
	tests := []struct {
		expression string
		want       string
	}{
		// The expression from #90: hour 25 was stored and then broke the schedule page.
		{"0 2 25 ? * * *", "hours"},
		{"0 2 * * 1", "6 or 7 fields"},
		{"@daily", "6 or 7 fields"},
		{"0 0 12 * * *", "exactly one of day of month and day of week"},
		{"0 0 12 ? * ?", "exactly one of day of month and day of week"},
		{"0 61 12 * * ?", "minutes"},
		{"0 0 12 * 13 ?", "month"},
		{"0 0 12 ? * 8", "day of week"},
		{"0 0 12 ? * MO", "day of week"},
		{"0 0 12 * * ?  ", "year"},
	}
	for _, test := range tests {
		err := validateQuartzCron(test.expression)
		if err == nil {
			t.Errorf("%q: expected an error", test.expression)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%q: error %q does not mention %q", test.expression, err, test.want)
		}
	}
}

func TestQuartzCronValidator_ValidateString(t *testing.T) {
	check := func(value types.String) validator.StringResponse {
		var resp validator.StringResponse
		quartzCronValidator{}.ValidateString(context.Background(), validator.StringRequest{
			Path:        path.Root("schedule"),
			ConfigValue: value,
		}, &resp)
		return resp
	}

	if resp := check(types.StringValue("0 0 2 * * ?")); resp.Diagnostics.HasError() {
		t.Errorf("a valid expression was rejected: %v", resp.Diagnostics)
	}
	// Null and unknown values are validated again once they are known.
	for _, value := range []types.String{types.StringNull(), types.StringUnknown()} {
		if resp := check(value); resp.Diagnostics.HasError() {
			t.Errorf("%v should not be validated: %v", value, resp.Diagnostics)
		}
	}

	resp := check(types.StringValue("0 2 25 ? * * *"))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error for hour 25")
	}
	detail := resp.Diagnostics.Errors()[0].Detail()
	if !strings.Contains(detail, "0 2 25 ? * * *") || !strings.Contains(detail, "hours") || !strings.Contains(detail, "Quartz") {
		t.Errorf("the error should name the expression, the field and Quartz, got: %s", detail)
	}
}

func TestWorkspaceScheduleResource_ScheduleIsValidated(t *testing.T) {
	ctx := context.Background()
	var resp resource.SchemaResponse
	(&WorkspaceScheduleResource{}).Schema(ctx, resource.SchemaRequest{}, &resp)

	attribute, ok := resp.Schema.Attributes["schedule"].(schema.StringAttribute)
	if !ok {
		t.Fatal("schedule is not a string attribute")
	}
	// The attribute must carry the Quartz validator, so a bad expression fails at plan time.
	found := false
	for _, v := range attribute.Validators {
		if _, ok := v.(quartzCronValidator); ok {
			found = true
		}
	}
	if !found {
		t.Error("the schedule attribute does not use the Quartz cron validator")
	}
}
