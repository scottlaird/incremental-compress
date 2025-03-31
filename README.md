# Incremental-Compress

This is a tool for incrementally (re)compressing static web page
files, so they can be served via web servers without requiring
on-the-fly compression.

It does this by walking through all files in a specified directory
tree and looking for commonly compressible file types (currently
.html, .css, .js, .json, .xml, .ico, .svg, and .md), and then
compressing each file individually using GZip, Brotli, and Zstd.

So, for a directory that contains three files:

- index.html
- images/pictures.html
- images/logo.png

incremental-compress will create the following additional files:

- index.html.br
- index.html.gz
- index.html.zst
- images/pictures.html.br
- images/pictures.html.gz
- images/pictures.html.zst

These can then be served transparently by many webservers to most
browsers without spending any additional CPU on compression.

This was written for serving Hugo-generated content via the Caddy web
server, but it should be compatible with a wide variety of web tools.

## Installing

Install a current version of Go, then run

```
$ go install github.com/scottlaird/incremental-compress@latest
```

This will write an `incremental-compress` executable into your Go
binary directory, typically `~/go/bin`.

## Usage

Run `incremental-compress --dir=<directory>` to recompress everything
in `<directory>` and its subdirectories.  By default, it uses gzip,
brotli, and zstd for each file, but these can be disabled via
`--gzip=false` and similar.

It defaults to the highest compression level supported by each tool;
these can be changed via flags.

By default, it compresses any files that end in `.html`, `.css`,
`.js`, `.json`, `.xml`, `.ico`, `.svg`, or `.md`.  This is controlled
via the `--types` flag.

If a compressed output file is the same age or newer than the source
file, then it won't be re-compressed.

Compression happens in parallel, using as many threads as Go thinks
your hardware can support.  My test machine was able to compress 4027
files in 69 seconds, using over 1200 seconds of CPU time.

If any errors occur then messages will be written to STDERR and the
exit code will be non-zero, but `incremental-compress` will try to
keep going and process other files.

## Configuring Web Servers for Pre-compressed content

### Caddy

Just add `precompressed` to your `file_server` directive, like this:

```caddy
:80 {
    root * /my/directory
    file_server {
        precompressed
    }
}
```

### Apache

Apache's
[mod_deflate](https://httpd.apache.org/docs/2.4/mod/mod_deflate.html)
and
[mod_brotli](https://httpd.apache.org/docs/2.4/mod/mod_brotli.html#precompressed)
module docs show how to set up pre-compressed content for specific
compression types and file formats, but it involves specific
configuration per file type and compression type.

### Nginx

I don't really use nginx these days, so this is untested.  It looks
like nginx only supports gzip compression out of the box, although
third-party modules are available for Brotli and Zstd.

According to the
[documentation](https://nginx.org/en/docs/http/ngx_http_gzip_static_module.html),
you should just be able to add this to your configs:

```
gzip_static  always;
gzip_proxied expired no-cache no-store private auth;
```

## Chosing a Compression Algorithm

See [Paul Calvano's article from
2024](https://paulcalvano.com/2024-03-19-choosing-between-gzip-brotli-and-zstandard-compression/)
for some context, although it's mostly focused on on-the-fly
compression, where CPU speed matters much more than output size.  For
pre-compressed static content, we can mostly ignore CPU time and just focus on compression size.

Gzip and Brotli are supported by all common browsers, including
Chrome, Safari, and Firefox versions newer than late 2017.  Zstd is
less widely supported, but mid-2024 or newer versions of Chrome and
Firefox should support it.

Using my website as an example:

- Compressible content: 62.2 MB
- gzip: 15.5 MB (25.0%)
- zstd: 14.6 MB (23.5%)
- brotli: 12.6 MB (20.2%)

Given these numbers, Brotli seems like a clear win for my uses, but
your milage may vary.
