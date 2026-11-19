// Package servertest is the black-box test harness: a Client that joins a
// server over a real connection and reads its packets, the helpers the test
// packages under internal/server/tests share, and the server they start through the
// hooks in internal/server/servertest_hooks.go. Importing it installs the flat test
// world (server.TestFlatLevelType).
package servertest
