package main

import (
        "compress/gzip"
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
        "crypto/sha1"
        "strconv"

        
        "github.com/DataDog/zstd" // much better compression than "github.com/klauspost/compress/zstd"
        "github.com/andybalholm/brotli"
        "github.com/chaisql/chai"
)

var (
        verboseFlag     = flag.Bool("verbose", false, "Show status")
        dirFlag         = flag.String("dir", ".", "Directory to search for compressable files")
        gzipFlag        = flag.Bool("gzip", true, "Compress with gzip")
        gzipLevelFlag   = flag.Int("gzip_level", 9, "gzip compression level")
        zstdFlag        = flag.Bool("zstd", true, "Compress with zstd")
        zstdLevelFlag   = flag.Int("zstd_level", 19, "zstd compression level")
        brotliFlag      = flag.Bool("brotli", true, "Compress with brotli")
        brotliLevelFlag = flag.Int("brotli_level", 11, "brotli compression level")
        typesFlag       = flag.String("types", "html,css,js,json,xml,ico,svg,md", "File types to compress, seperated by a comma")
        stateDirFlag    = flag.String("statedir", "", "Directory saving checksums and other state across runs")
        preserveMtimeFlag = flag.Bool("preserve_mtime", true, "Preserve mtime for files with the same checksum")

        types   []string
        logger  *log.Logger
        failure = false
        wg      sync.WaitGroup

        state = "Starting"
        countMutex       sync.Mutex
        totalFileCount   = 0
        pendingFileCount = 0
)

func start() {
        wg.Add(1)
        countMutex.Lock()
        totalFileCount++
        pendingFileCount++
        countMutex.Unlock()
}

func done() {
        wg.Done()
        countMutex.Lock()
        pendingFileCount--
        countMutex.Unlock()
}

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

}

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
}

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
}

func walkFile(db *chai.DB, path string, info fs.FileInfo, err error) error {
        if info.IsDir() {
                return nil
        }

        for _, fileType := range types {
                if strings.HasSuffix(path, "."+fileType) {
                        return maybeCompressFile(db, path, info)
                }
        }
        return nil
}

func checkCompressedFile(db *chai.DB, path string, info fs.FileInfo, extension string) bool {
        compressed, err := os.Stat(path + extension)

        if err != nil || compressed.ModTime().Before(info.ModTime()) {
                return true
        }
        return false
}


func checkSourceFile(db *chai.DB, path string, info fs.FileInfo) bool {
        if db != nil {
                sha := sha1.New()
                data, err := os.ReadFile(path)
                if err != nil {
                        panic(err)
                }
                sha.Write(data)
                checksum := fmt.Sprintf("%x", sha.Sum(nil))

                row, err := db.QueryRow("select mtime from checksumstate where checksum=? and filename=?", checksum, path)
                if err != nil {
                        if fmt.Sprintf("%v", err) == "row not found" {  // so ugly
                                if *verboseFlag {
                                        fmt.Printf("Didn't find %s with a checkum of %s (%v), inserting!\n", path, checksum, err)
                                }
                                err := db.Exec(`delete from checksumstate where filename=?`, path)
                                if err != nil { panic(err) }
                                err = db.Exec(`insert into checksumstate values (?, ?, ?)`, path, checksum, info.ModTime().Unix() )
                                if err != nil { panic(err) }
                                return true
                        } else {
                                log.Printf("Error checking database for state: %v", err)
                        }
                }
                o := row.Object()
                value, err := o.GetByField("mtime")
                if err != nil {
                        panic(err)
                }
                v := value.String()
                i, _ := strconv.ParseInt(v, 10, 64)
                t := time.Unix(i, 0)

                if t != info.ModTime() {
                        if *verboseFlag {
                                logger.Printf("Changing mtime of %q from %s to %s", path, info.ModTime().String(), t.String())
                        }
                        err = os.Chtimes(path, t, t)
                        if err != nil {
                                panic(err)
                        }
                }
                
                
                return false
        }
        return false
}

func maybeCompressFile(db *chai.DB, path string, info fs.FileInfo) error {
        //fmt.Printf("MaybeCompress %q\n", path)

        forceRecompress := checkSourceFile(db, path, info)
        info, _ = os.Stat(path)

        if *gzipFlag && (forceRecompress || checkCompressedFile(db, path, info, ".gz")) {
                start()
                go doGzip(path, info)
        }
        if *zstdFlag && (forceRecompress || checkCompressedFile(db, path, info, ".zst")) {
                start()
                go doZstd(path, info)
        }
        if *brotliFlag && (forceRecompress || checkCompressedFile(db, path, info, ".br")) {
                start()
                go doBrotli(path, info)
        }
        return nil
}

func printStatus() {
        known := "known "
        for {
                if state == "Compressing" {
                        known =""
                }
                fmt.Printf("%s, %d of %d %sfiles queued to be compressed           \r", state, pendingFileCount, totalFileCount, known)
                time.Sleep(50*time.Millisecond)
        }
}

func main() {
        flag.Parse()
        types = strings.Split(*typesFlag, ",")
        logger = log.New(os.Stderr, "", 0)

        var db *chai.DB
        var err error

        if *stateDirFlag != "" {
                db, err = chai.Open(*stateDirFlag)
                if err != nil {
                        panic(err)
                }
                defer db.Close()

                db.Exec(` CREATE TABLE checksumstate ( filename text primary key, checksum text, mtime int )`)

        }

        go printStatus()

        state = "Finding files"
        err = filepath.Walk(*dirFlag, func(path string, info fs.FileInfo, err error) error { walkFile(db, path, info, err); return nil })
        if err != nil {
                panic(err)
        }
        state = "Compressing"

        wg.Wait()

        state = "Exiting"

        if failure {
                os.Exit(1)
        }

        fmt.Printf("\n")
}
