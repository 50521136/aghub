/**
 * The request to test acceleration proxies.  Testing is done in batches, so
 * the caller sends at most forty prefixes at a time.
 */
export interface UpdateTestProxiesRequest {
    proxies: string[];
}
