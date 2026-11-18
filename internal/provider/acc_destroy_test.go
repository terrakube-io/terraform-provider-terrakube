package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// CheckDestroy helpers for the acceptance tests. Terrakube soft-deletes some objects (an
// organization is disabled and renamed), so they check what the API returns, not the database.

// testAccCheckDestroyed returns a CheckDestroy that runs gone for every resource of
// resourceType left in the state and fails when the API still returns one of them.
func testAccCheckDestroyed(resourceType string, gone func(rs *terraform.ResourceState) (bool, error)) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		for _, rs := range s.RootModule().Resources {
			if rs.Type != resourceType {
				continue
			}
			ok, err := gone(rs)
			if err != nil {
				return fmt.Errorf("checking %s %s: %w", resourceType, rs.Primary.ID, err)
			}
			if !ok {
				return fmt.Errorf("%s %s still exists", resourceType, rs.Primary.ID)
			}
		}
		return nil
	}
}

// testAccGoneWhenNotFound treats an object as gone when GET on the path returns 404.
func testAccGoneWhenNotFound(path func(rs *terraform.ResourceState) string) func(rs *terraform.ResourceState) (bool, error) {
	return func(rs *terraform.ResourceState) (bool, error) {
		status, _, err := testAccAPIGet(path(rs))
		if err != nil {
			return false, err
		}
		switch status {
		case http.StatusNotFound:
			return true, nil
		case http.StatusOK:
			return false, nil
		default:
			return false, fmt.Errorf("unexpected status %d", status)
		}
	}
}

// testAccWorkspaceGone treats a workspace as gone when GET by id returns 404. Any terrakube_workspace_*
// resource works, since they all keep organization_id in state.
var testAccWorkspaceGone = testAccGoneWhenNotFound(func(rs *terraform.ResourceState) string {
	return "/api/v1/organization/" + rs.Primary.Attributes["organization_id"] + "/workspace/" + rs.Primary.ID
})

// testAccOrganizationGone treats an organization as gone when the organization list no longer
// returns it. Deleting an organization disables it; GET by id still returns the disabled one.
func testAccOrganizationGone(rs *terraform.ResourceState) (bool, error) {
	status, body, err := testAccAPIGet("/api/v1/organization")
	if err != nil {
		return false, err
	}
	if status != http.StatusOK {
		return false, fmt.Errorf("listing organizations: status %d", status)
	}
	var list struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return false, fmt.Errorf("decoding organization list: %w", err)
	}
	for _, organization := range list.Data {
		if organization.ID == rs.Primary.ID {
			return false, nil
		}
	}
	return true, nil
}

// testAccAPIGet calls the Terrakube API the acceptance tests run against.
func testAccAPIGet(path string) (int, []byte, error) {
	request, err := http.NewRequest(http.MethodGet, os.Getenv("TERRAKUBE_ENDPOINT")+path, nil)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv("TERRAKUBE_TOKEN"))
	request.Header.Set("Accept", "application/vnd.api+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return response.StatusCode, body, err
}

// testAccImportID returns an import ID made of the given attributes of the resource and then its
// id, joined with commas, for example "organization_id,id".
func testAccImportID(resourceName string, attrs ...string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return "", fmt.Errorf("%s not found in state", resourceName)
		}
		parts := make([]string, 0, len(attrs)+1)
		for _, attr := range attrs {
			parts = append(parts, rs.Primary.Attributes[attr])
		}
		return strings.Join(append(parts, rs.Primary.ID), ","), nil
	}
}
