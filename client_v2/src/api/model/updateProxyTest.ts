/**
 * The result of testing one acceleration proxy from this server.
 */
export interface UpdateProxyTest {
    url: string;
    host: string;

    /** True when the proxy served the GitHub API. */
    api_ok?: boolean;

    /** True when the proxy served a release asset. */
    download_ok?: boolean;

    /** Round-trip time of the successful request, in milliseconds. */
    latency_ms?: number;

    error?: string;
}
