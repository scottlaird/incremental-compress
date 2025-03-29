package main

import (
        "compress/gzip"
        "crypto/sha1"
        "flag"
        "fmt"
        "io"
        "io/fs"
        "log"
        "os"
        "path/filepath"
        "strings"
        "sync"
        "time"

        "github.com/DataDog/zstd" // much better compression than "github.com/klauspost/compress/zstd"
        "github.com/andybalholm/brotli"
        "zombiezen.com/go/sqlite"
        "zombiezen.com/go/sqlite/sqlitex"
)

var (
        quietFlag         = flag.Bool("quiet", false, "Avoid printing status updates")
        verboseFlag       = flag.Bool("verbose", false, "Show status")
        dirFlag           = flag.String("dir", ".", "Directory to search for compressable files")
        gzipFlag          = flag.Bool("gzip", true, "Compress with gzip")
        gzipLevelFlag     = flag.Int("gzip_level", 9, "gzip compression level")
        zstdFlag          = flag.Bool("zstd", true, "Compress with zstd")
        zstdLevelFlag     = flag.Int("zstd_level", 19, "zstd compression level")
        brotliFlag        = flag.Bool("brotli", true, "Compress with brotli")
        brotliLevelFlag   = flag.Int("brotli_level", 11, "brotli compression level")
        typesFlag         = flag.String("types", "html,css,js,json,xml,ico,svg,md", "File types to compress, seperated by a comma")
        stateDirFlag      = flag.String("statedir", "", "Directory saving checksums and other state across runs")
        preserveMtimeFlag = flag.Bool("preserve_mtime", true, "Preserve mtime for files with the same checksum")

        types   []string
        logger  *log.Logger
        failure = false
        wg      sync.WaitGroup

        state                     = "Starting"
        countMutex                sync.Mutex
        totalFileCount            = 0
        compressedFileUpdateCount = 0
        handledFileCount          = 0
        pendingFileCount          = 0
        checksummedCount          = 0
)

func foundFile() {
        countMutex.Lock()
        totalFileCount++
        countMutex.Unlock()
}

// Start processing a file.
func start() {
        wg.Add(1)
        countMutex.Lock()
        handledFileCount++
        pendingFileCount++
        countMutex.Unlock()
}

// Mark a file as complete.
func done() {
        wg.Done()
        countMutex.Lock()
        pendingFileCount--
        countMutex.Unlock()
}

func compressed() {
        countMutex.Lock()
        compressedFileUpdateCount++
        countMutex.Unlock()
}

func checksummed() {
        countMutex.Lock()
        checksummedCount++
        countMutex.Unlock()
}

// Compress a file using gzip, reusing the times and permissions of
// the original file.
func doGzip(path string, info fs.FileInfo) {
        defer done()
        outpath := path + ".gz"

        if *verboseFlag {
                logger.Printf("gzip %q", path)
        }

        reader, err := os.Open(path)
        if err != nil {
                logger.Fatalf("Could not open %q for reading: %v", path, err)
                failure = true
                return
        }
        defer reader.Close()

        outfile, err := os.Create(outpath)
        if err != nil {
                logger.Fatalf("Could not open %q for writing: %v", outpath, err)
                failure = true
                return
        }
        defer outfile.Close()

        writer, err := gzip.NewWriterLevel(outfile, *gzipLevelFlag)
        if err != nil {
                logger.Fatalf("Could not create gzip writer for %q: %v", path, err)
                failure = true
                return
        }
        _, err = io.Copy(writer, reader)
        if err != nil {
                logger.Fatalf("Could not copy data for %q: %v", path, err)
                failure = true
                return
        }
        writer.Close() // force a flush

        err = os.Chtimes(outpath, info.ModTime(), info.ModTime())
        if err != nil {
                logger.Fatalf("Could not update times for %q: %v", outpath, err)
                failure = true
                return
        }

        err = os.Chmod(outpath, info.Mode())
        if err != nil {
                logger.Fatalf("Could not update modes for %q: %v", outpath, err)
                failure = true
                return
        }

        compressed()
}

// Compress a file using zstd, reusing the times and permissions of
// the original file.
func doZstd(path string, info fs.FileInfo) {
        defer done()
        outpath := path + ".zst"

        if *verboseFlag {
                logger.Printf("zstd %q", path)
        }

        reader, err := os.Open(path)
        if err != nil {
                logger.Fatalf("Could not open %q for reading: %v", path, err)
                failure = true
                return
        }
        defer reader.Close()

        outfile, err := os.Create(outpath)
        if err != nil {
                logger.Fatalf("Could not open %q for writing: %v", outpath, err)
                failure = true
                return
        }
        defer outfile.Close()

        writer := zstd.NewWriterLevel(outfile, *zstdLevelFlag)

        _, err = io.Copy(writer, reader)
        if err != nil {
                logger.Fatalf("Could not copy data for %q: %v", path, err)
                failure = true
                return
        }
        writer.Close() // force a flush

        err = os.Chtimes(outpath, info.ModTime(), info.ModTime())
        if err != nil {
                logger.Fatalf("Could not update times for %q: %v", outpath, err)
                failure = true
                return
        }

        err = os.Chmod(outpath, info.Mode())
        if err != nil {
                logger.Fatalf("Could not update modes for %q: %v", outpath, err)
                failure = true
                return
        }
        compressed()
}

// Compress a file using brotli, reusing the times and permissions of
// the original file.
func doBrotli(path string, info fs.FileInfo) {
        defer done()
        outpath := path + ".br"

        if *verboseFlag {
                fmt.Printf("zstd %q\n", path)
        }

        reader, err := os.Open(path)
        if err != nil {
                logger.Fatalf("Could not open %q for reading: %v", path, err)
                failure = true
                return
        }
        defer reader.Close()

        outfile, err := os.Create(outpath)
        if err != nil {
                logger.Fatalf("Could not open %q for writing: %v", outpath, err)
                failure = true
                return
        }
        defer outfile.Close()

        writer := brotli.NewWriterLevel(outfile, *brotliLevelFlag)

        _, err = io.Copy(writer, reader)
        if err != nil {
                logger.Fatalf("Could not copy data for %q: %v", path, err)
                failure = true
                return
        }
        writer.Close() // force a flush

        err = os.Chtimes(outpath, info.ModTime(), info.ModTime())
        if err != nil {
                logger.Fatalf("Could not update times for %q: %v", outpath, err)
                failure = true
                return
        }

        err = os.Chmod(outpath, info.Mode())
        if err != nil {
                logger.Fatalf("Could not update modes for %q: %v", outpath, err)
                failure = true
                return
        }
        compressed()
}

// Process a single file.  Used as a callback (indirectly) from filepath.Walk.
func walkFile(conn *sqlite.Conn, path string, info fs.FileInfo, err error) error {
        if info.IsDir() {
                return nil
        }

        for _, fileType := range types {
                if strings.HasSuffix(path, "."+fileType) {
                        foundFile()
                        return maybeCompressFile(conn, path, info)
                        //return nil
                }
        }
        return nil
}

// Check a compressed file to see if it needs to be rebuilt.
func checkCompressedFile(conn *sqlite.Conn, path string, info fs.FileInfo, extension string) bool {
        compressed, err := os.Stat(path + extension)

        if err != nil || compressed.ModTime().Before(info.ModTime()) {
                return true
        }
        return false
}

// Check a source file to see if it needs to be rebuilt
func checkSourceFile(conn *sqlite.Conn, path string, info fs.FileInfo) bool {
        if conn != nil {
                sha := sha1.New()

                file, err := os.Open(path)
                if err != nil {
                        panic(err)
                }
                io.Copy(sha, file)
                file.Close()

                checksum := fmt.Sprintf("%x", sha.Sum(nil))
                checksummed()

                stmt, err := conn.Prepare("select mtime from checksumstate where checksum=$checksum and filename=$filename;")
                if err != nil {
                        panic(err)
                }
                stmt.SetText("$checksum", checksum)
                stmt.SetText("$filename", path)

                var mtime time.Time

                hasRow, err := stmt.Step()

                if err != nil {
                        panic(err)
                }

                if hasRow {
                        v := stmt.GetInt64("mtime")
                        mtime = time.Unix(v, 0)
                } else {
                        if *verboseFlag {
                                fmt.Printf("Didn't find %s with a checkum of %s (%v), inserting!\n", path, checksum, err)
                        }
                        err = sqlitex.ExecuteTransient(
                                conn,
                                "delete from checksumstate where filename=?;",
                                &sqlitex.ExecOptions{
                                        Args: []any{path},
                                },
                        )
                        if err != nil {
                                panic(err)
                        }

                        err = sqlitex.ExecuteTransient(
                                conn,
                                `insert into checksumstate values (?, ?, ?);`,
                                &sqlitex.ExecOptions{
                                        Args: []any{path, checksum, info.ModTime().Unix()},
                                })

                        if err != nil {
                                panic(err)
                        }
                        return true
                }

                if mtime != info.ModTime() {
                        if *verboseFlag {
                                logger.Printf("Changing mtime of %q from %s to %s", path, info.ModTime().String(), mtime.String())
                        }
                        err = os.Chtimes(path, mtime, mtime)
                        if err != nil {
                                panic(err)
                        }
                }
                return false
        }
        return false
}

func maybeCompressFile(conn *sqlite.Conn, path string, info fs.FileInfo) error {
        forceRecompress := checkSourceFile(conn, path, info)
        info, _ = os.Stat(path)

        if *gzipFlag && (forceRecompress || checkCompressedFile(conn, path, info, ".gz")) {
                start()
                go doGzip(path, info)
        }
        if *zstdFlag && (forceRecompress || checkCompressedFile(conn, path, info, ".zst")) {
                start()
                go doZstd(path, info)
        }
        if *brotliFlag && (forceRecompress || checkCompressedFile(conn, path, info, ".br")) {
                start()
                go doBrotli(path, info)
        }
        return nil
}

func writeStatusMessage() {
        if !*quietFlag {
                fmt.Fprintf(os.Stderr, "%s, %d compressed files updated, %d source files queued to compress, %d checked so far           \r", state, compressedFileUpdateCount, pendingFileCount, totalFileCount)
        }
}

func printStatus() {
        for {
                writeStatusMessage()
                time.Sleep(50 * time.Millisecond)
        }
}

func main() {
        flag.Parse()
        types = strings.Split(*typesFlag, ",")
        logger = log.New(os.Stderr, "", 0)

        var conn *sqlite.Conn
        var err error

        if *stateDirFlag != "" {
                conn, err = sqlite.OpenConn(*stateDirFlag)
                if err != nil {
                        panic(err)
                }
                defer conn.Close()

                err := sqlitex.ExecuteScript(conn, `CREATE TABLE IF NOT EXISTS checksumstate ( filename text primary key, checksum text, mtime int )`, &sqlitex.ExecOptions{})
                if err != nil {
                        panic(err)
                }

        }

        go printStatus()

        state = "Finding files"
        err = filepath.Walk(*dirFlag, func(path string, info fs.FileInfo, err error) error { walkFile(conn, path, info, err); return nil })
        if err != nil {
                panic(err)
        }
        state = "Compressing"

        wg.Wait()

        state = "Exiting"

        if failure {
                os.Exit(1)
        }

        if !*quietFlag {
                writeStatusMessage()
                fmt.Fprintf(os.Stderr, "\n")
        }
}
