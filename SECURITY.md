# Security policy

## Supported versions

Security fixes go into the latest released minor version.

## Reporting a vulnerability

Please do not open a public issue. Report privately through GitHub:
**Security → Report a vulnerability** on
<https://github.com/nayan9229/jsonflat/security/advisories/new>.

Include the config, an input that triggers the problem, and what you observed.
You can expect a first answer within a week.

## What counts

`jsonflat` parses untrusted JSON, so these are treated as security issues:

- a panic, an unbounded loop or unbounded memory growth on any input;
- output that is not valid JSON;
- output, a `Record`, or pooled state that leaks data from one call into
  another, or that aliases the caller's input;
- a data race on a `Transformer` that is used as documented.

## Limits you should know about

- Memory use is proportional to the largest document parsed. Put a size limit
  on request bodies before they reach `jsonflat`. State from inputs above
  4 MiB is not pooled, so one large document does not pin memory.
- Nesting deeper than 300 levels is rejected by the parser.
- Invalid UTF-8 inside strings is passed through unchanged. Validate it
  downstream if your sink requires valid UTF-8.
- Collision tracking compares 64-bit hashes of keys. Someone who controls the
  keys of a record could, with considerable effort, construct two different
  keys with the same hash, and the second would be treated as a duplicate of
  the first. The effect is limited to that one row.
