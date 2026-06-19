package sqlx

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Logger controls SQL statement logging. It is safe to modify the fields of the
// global Log instance from any goroutine after initialization, but not while
// SQL statements are being executed concurrently.
type Logger struct {
	// Output is the destination for SQL log lines. Defaults to os.Stdout.
	Output io.Writer

	// Enabled controls whether SQL logging is performed. Defaults to true.
	Enabled bool
}

// Log is the global logger used by all DB and Engine operations. Set
// Log.Output to redirect SQL logs, or set Log.Enabled to false to disable
// logging entirely.
//
//	// Redirect to file
//	f, _ := os.OpenFile("sql.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
//	sqlx.Log.Output = f
//
//	// Disable
//	sqlx.Log.Enabled = false
var Log = &Logger{
	Output:  os.Stdout,
	Enabled: true,
}

func (l *Logger) logQuery(query string, args []interface{}, d time.Duration, err error) {
	if !l.Enabled || l.Output == nil {
		return
	}
	prefix := "[sqlx]"
	if err != nil {
		fmt.Fprintf(l.Output, "%s[%.1fms] ERROR %s %v err=%v\n", prefix, float64(d.Microseconds())/1000, query, args, err)
		return
	}
	fmt.Fprintf(l.Output, "%s[%.1fms] %s %v\n", prefix, float64(d.Microseconds())/1000, query, args)
}

func (l *Logger) logExec(query string, args []interface{}, d time.Duration, affected int64, err error) {
	if !l.Enabled || l.Output == nil {
		return
	}
	prefix := "[sqlx]"
	if err != nil {
		fmt.Fprintf(l.Output, "%s[%.1fms] ERROR %s %v err=%v\n", prefix, float64(d.Microseconds())/1000, query, args, err)
		return
	}
	fmt.Fprintf(l.Output, "%s[%.1fms] %s %v affected:%d\n", prefix, float64(d.Microseconds())/1000, query, args, affected)
}
