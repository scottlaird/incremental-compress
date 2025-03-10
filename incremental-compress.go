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

        "github.com/andybalholm/brotli"
        "github.com/DataDog/zstd"  // much better compression than "github.com/klauspost/compress/zstd"
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

        types   []string
        logger  *log.Logger
        failure = false
        wg      sync.WaitGroup

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
                fmt.Printf("gzip %q\n", path)
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

func walkFile(path string, info fs.FileInfo, err error) error {
        if info.IsDir() {
                return nil
        }

        for _, fileType := range types {
                if strings.HasSuffix(path, "."+fileType) {
                        return maybeCompressFile(path, info)
                }
        }
        return nil
}

func checkCompressedFile(path string, info fs.FileInfo, extension string) bool {
        compressed, err := os.Stat(path + extension)
        if err != nil || compressed.ModTime().Before(info.ModTime()) {
                return true
        }
        return false
}

func maybeCompressFile(path string, info fs.FileInfo) error {
        //fmt.Printf("MaybeCompress %q\n", path)

        if *gzipFlag && checkCompressedFile(path, info, ".gz") {
                start()
                go doGzip(path, info)
        }
        if *zstdFlag && checkCompressedFile(path, info, ".zst") {
                start()
                go doZstd(path, info)
        }
        if *brotliFlag && checkCompressedFile(path, info, ".br") {
                start()
                go doBrotli(path, info)
        }
        return nil
}

func printStatus() {
        for {
                fmt.Printf("Compressing, %d of %d files remain\n", pendingFileCount, totalFileCount)
                time.Sleep(time.Second)

                if pendingFileCount == 0 {
                        return
                }
        }
}

func main() {
        flag.Parse()
        types = strings.Split(*typesFlag, ",")
        logger = log.New(os.Stderr, "", 0)

        err := filepath.Walk(*dirFlag, walkFile)
        if err != nil {
                panic(err)
        }

        printStatus()

        wg.Wait()

        if failure {
                os.Exit(1)
        }
}
