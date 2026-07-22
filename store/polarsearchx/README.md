# polarsearchx

`polarsearchx` is a small wrapper around the OpenSearch-compatible API exposed by Aliyun PolarSearch. It is for PolarSearch clusters, not Aliyun OpenSearch application-search apps.

The package keeps request bodies native to OpenSearch DSL and returns typed response envelopes while preserving raw request access for index operations and diagnostics.

```go
client, err := polarsearchx.New(&polarsearchx.Config{
    Addresses: []string{"http://your-polarsearch-endpoint:3001"},
    Username:  "user",
    Password:  "password",
    Timeout:   3 * time.Second,
})
if err != nil {
    return err
}

resp, err := client.Search(ctx, "plaza_product_search", map[string]any{
    "size": 10,
    "_source": []string{"product_spu_id"},
    "query": map[string]any{
        "match": map[string]any{"all_text": "苹果"},
    },
})
```

Every address must be an absolute `http` or `https` URL. Aliyun exposes the PolarSearch connection address as `HOST:PORT`; callers must add the protocol explicitly before constructing the client.

Use `Raw` when the caller needs a PolarSearch/OpenSearch endpoint that should stay outside the wrapper's typed surface.
