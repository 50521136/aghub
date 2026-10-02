/**
 * The request to set the acceleration proxy.
 */
export interface UpdateSetProxyRequest {
    /** The prefix, or an empty string to disable the acceleration. */
    proxy: string;
}
