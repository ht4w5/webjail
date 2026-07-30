package parser

// See https://www.iana.org/assignments/http-methods/http-methods.xhtml.
var methodMap = map[string]string{
	"ACL":               "ACL",
	"BASELINE-CONTROL":  "BASELINE-CONTROL",
	"BIND":              "BIND",
	"CHECKIN":           "CHECKIN",
	"CHECKOUT":          "CHECKOUT",
	"CONNECT":           "CONNECT",
	"COPY":              "COPY",
	"DELETE":            "DELETE",
	"GET":               "GET",
	"HEAD":              "HEAD",
	"LABEL":             "LABEL",
	"LINK":              "LINK",
	"LOCK":              "LOCK",
	"MERGE":             "MERGE",
	"MKACTIVITY":        "MKACTIVITY",
	"MKCALENDAR":        "MKCALENDAR",
	"MKCOL":             "MKCOL",
	"MKREDIRECTREF":     "MKREDIRECTREF",
	"MKWORKSPACE":       "MKWORKSPACE",
	"MOVE":              "MOVE",
	"OPTIONS":           "OPTIONS",
	"ORDERPATCH":        "ORDERPATCH",
	"PATCH":             "PATCH",
	"POST":              "POST",
	"PRI":               "PRI",
	"PROPFIND":          "PROPFIND",
	"PROPPATCH":         "PROPPATCH",
	"PUT":               "PUT",
	"QUERY":             "QUERY",
	"REBIND":            "REBIND",
	"REPORT":            "REPORT",
	"SEARCH":            "SEARCH",
	"TRACE":             "TRACE",
	"UNBIND":            "UNBIND",
	"UNCHECKOUT":        "UNCHECKOUT",
	"UNLINK":            "UNLINK",
	"UNLOCK":            "UNLOCK",
	"UPDATE":            "UPDATE",
	"UPDATEREDIRECTREF": "UPDATEREDIRECTREF",
	"VERSION-CONTROL":   "VERSION-CONTROL",
}

// Converts method string to static string.
func method(str string) string {
	if m, ok := methodMap[str]; ok {
		return m
	}
	return str
}
