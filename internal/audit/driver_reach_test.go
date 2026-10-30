package audit

import (
	"strings"
	"testing"
)

// THE DRIVER REACH GUARD.
//
// The database drivers stay in the root module (splitting them into modules of
// their own was declined), so what keeps a consumer from building a driver it
// never uses is the import graph alone: Go compiles only the packages a build
// imports. That holds while two rules do, and nothing else enforces them:
//
//   - only dao/sqlite, dao/postgres and dao/mysql (and packages under them)
//     import a database driver;
//   - only packages under dao import a dao/<engine> package.
//
// Then a consumer that imports no dao/<engine> package builds no driver,
// whatever golib's other packages do in future. Test files are not counted:
// they are not part of what a consumer links.

// databaseDrivers are the driver modules: an import of one, or of a package
// under it, is a driver import.
var databaseDrivers = []string{
	"modernc.org/sqlite",
	"github.com/mattn/go-sqlite3",
	"github.com/jackc/pgx",
	"github.com/lib/pq",
	"github.com/go-sql-driver/mysql",
}

// driverOwners are the packages a driver import is allowed in.
var driverOwners = []string{modulePath + "/dao/sqlite", modulePath + "/dao/postgres", modulePath + "/dao/mysql"}

// daoEngines are the dao/<engine> packages, each bringing its driver or client.
var daoEngines = []string{
	modulePath + "/dao/sqlite", modulePath + "/dao/postgres", modulePath + "/dao/mysql", modulePath + "/dao/bigquery",
}

// under reports whether pkg is one of roots or a package below one.
func under(pkg string, roots ...string) bool {
	for _, r := range roots {
		if pkg == r || strings.HasPrefix(pkg, r+"/") {
			return true
		}
	}
	return false
}

func TestDriversReachOnlyTheirImporters(t *testing.T) {
	graph := packageImports(t, repoRoot(t), func(string) bool { return true })
	for pkg, imports := range graph {
		for _, imp := range imports {
			if under(imp, databaseDrivers...) && !under(pkg, driverOwners...) {
				t.Errorf("%s imports the database driver %s.\n"+
					"Only dao/sqlite, dao/postgres and dao/mysql may: a consumer importing %s would build the driver.", pkg, imp, pkg)
			}
			if under(imp, daoEngines...) && !under(pkg, modulePath+"/dao") {
				t.Errorf("%s imports %s, outside dao.\n"+
					"A consumer importing %s would build that engine's driver without asking for it.", pkg, imp, pkg)
			}
		}
	}
}

// TestDriverReachGuardSeesTheDrivers is the positive control: a guard that
// walked an empty graph, or matched no driver, would pass for the wrong reason.
// Each owner must be seen importing its driver.
func TestDriverReachGuardSeesTheDrivers(t *testing.T) {
	graph := packageImports(t, repoRoot(t), func(string) bool { return true })
	for owner, driver := range map[string]string{
		modulePath + "/dao/sqlite":   "modernc.org/sqlite",
		modulePath + "/dao/postgres": "github.com/jackc/pgx",
		modulePath + "/dao/mysql":    "github.com/go-sql-driver/mysql",
	} {
		seen := false
		for _, imp := range graph[owner] {
			seen = seen || under(imp, driver)
		}
		if !seen {
			t.Errorf("%s is not seen importing %s: the guard is blind to it", owner, driver)
		}
	}
}
