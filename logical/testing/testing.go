package testing

import (
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/http"
	"github.com/hashicorp/vault/logical"
	"github.com/hashicorp/vault/physical"
	"github.com/hashicorp/vault/vault"
)

// TestEnvVar must be set to a non-empty value for acceptance tests to run.
const TestEnvVar = "TF_ACC"

// TestCase is a single set of tests to run for a backend. A TestCase
// should generally map 1:1 to each test method for your acceptance
// tests.
type TestCase struct {
	// Precheck, if non-nil, will be called once before the test case
	// runs at all. This can be used for some validation prior to the
	// test running.
	PreCheck func()

	// Backend is the backend that will be mounted.
	Backend logical.Backend

	// Steps are the set of operations that are run for this test case.
	Steps []TestStep

	// Teardown will be called before the test case is over regardless
	// of if the test succeeded or failed. This should return an error
	// in the case that the test can't guarantee all resources were
	// properly cleaned up.
	Teardown TestTeardownFunc
}

// TestStep is a single step within a TestCase.
type TestStep struct {
	// Operation is the operation to execute
	Operation logical.Operation

	// Path is the request path. The mount prefix will be automatically added.
	Path string

	// Arguments to pass in
	Data map[string]interface{}

	// Check is called after this step is executed in order to test that
	// the step executed successfully. If this is not set, then the next
	// step will be called
	Check TestCheckFunc
}

// TestCheckFunc is the callback used for Check in TestStep.
type TestCheckFunc func(*logical.Response) error

// TestTeardownFunc is the callback used for Teardown in TestCase.
type TestTeardownFunc func() error

// Test performs an acceptance test on a backend with the given test case.
//
// Tests are not run unless an environmental variable "TF_ACC" is
// set to some non-empty value. This is to avoid test cases surprising
// a user by creating real resources.
//
// Tests will fail unless the verbose flag (`go test -v`, or explicitly
// the "-test.v" flag) is set. Because some acceptance tests take quite
// long, we require the verbose flag so users are able to see progress
// output.
func Test(t TestT, c TestCase) {
	// We only run acceptance tests if an env var is set because they're
	// slow and generally require some outside configuration.
	if os.Getenv(TestEnvVar) == "" {
		t.Skip(fmt.Sprintf(
			"Acceptance tests skipped unless env '%s' set",
			TestEnvVar))
		return
	}

	// We require verbose mode so that the user knows what is going on.
	if !testTesting && !testing.Verbose() {
		t.Fatal("Acceptance tests must be run with the -v flag on tests")
		return
	}

	// Run the PreCheck if we have it
	if c.PreCheck != nil {
		c.PreCheck()
	}

	// Create an in-memory Vault core
	core, err := vault.NewCore(&vault.CoreConfig{
		Physical: physical.NewInmem(),
		LogicalBackends: map[string]logical.Factory{
			"test": func(map[string]string) (logical.Backend, error) {
				return c.Backend, nil
			},
		},
	})
	if err != nil {
		t.Fatal("error initializing core: ", err)
	}

	// Initialize the core
	init, err := core.Initialize(&vault.SealConfig{
		SecretShares:    1,
		SecretThreshold: 1,
	})
	if err != nil {
		t.Fatal("error initializing core: ", err)
	}

	// Unseal the core
	if sealed, err := core.Unseal(init.SecretShares[0]); err != nil {
		t.Fatal("error unsealing core: ", err)
	} else if sealed {
		t.Fatal("vault shouldn't be sealed")
	}

	// Create an HTTP API server and client
	ln, addr := http.TestServer(nil, core)
	defer ln.Close()
	clientConfig := api.DefaultConfig()
	clientConfig.Address = addr
	client, err := api.NewClient(clientConfig)
	if err != nil {
		t.Fatal("error initializing HTTP client: ", err)
	}

	// Mount the backend
	prefix := "mnt"
	if err := client.Sys().Mount(prefix, "test", "acceptance test"); err != nil {
		t.Fatal("error mounting backend: ", err)
	}

	// Make requests
	var revoke []*logical.Request
	for i, s := range c.Steps {
		log.Printf("[WARN] Executing test step %d", i+1)

		// Make sure to prefix the path with where we mounted the thing
		path := fmt.Sprintf("%s/%s", prefix, s.Path)

		// Create the request
		req := &logical.Request{
			Operation: s.Operation,
			Path:      path,
			Data:      s.Data,
		}

		// Make the request
		resp, err := core.HandleRequest(req)
		if resp != nil && resp.Secret != nil {
			// Revoke this secret later
			revoke = append(revoke, logical.RevokeRequest(
				req.Path,
				resp.Secret,
				resp.Data,
			))
		}
		if err == nil && s.Check != nil {
			// Call the test method
			err = s.Check(resp)
		}
		if err != nil {
			t.Error(fmt.Sprintf("Failed step %d: %s", i+1, err))
			break
		}
	}

	// Revoke any secrets we might have.
	var failedRevokes []*logical.Secret
	for _, req := range revoke {
		log.Printf("[WARN] Revoking secret: %#v", req.Secret)
		_, err = core.HandleRequest(req)
		if err != nil {
			failedRevokes = append(failedRevokes, req.Secret)
			t.Error(fmt.Sprintf("[ERR] Revoke error: %s", err))
		}
	}

	// Perform any rollbacks. This should no-op if there aren't any.
	log.Printf("[WARN] Requesting RollbackOperation")
	_, err = core.HandleRequest(logical.RollbackRequest(prefix + "/"))
	if err != nil {
		t.Error(fmt.Sprintf("[ERR] Rollback error: %s", err))
	}

	// If we have any failed revokes, log it.
	if len(failedRevokes) > 0 {
		for _, s := range failedRevokes {
			t.Error(fmt.Sprintf(
				"WARNING: Revoking the following secret failed. It may\n"+
					"still exist. Please verify:\n\n%#v",
				s))
		}
	}

	// Cleanup
	if c.Teardown != nil {
		c.Teardown()
	}
}

// TestT is the interface used to handle the test lifecycle of a test.
//
// Users should just use a *testing.T object, which implements this.
type TestT interface {
	Error(args ...interface{})
	Fatal(args ...interface{})
	Skip(args ...interface{})
}

var testTesting = false