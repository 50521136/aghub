/**
 * A request to send a test message through the configured mail server
 */
export interface PortalMailTestRequest {
    /** The address to send the test message to. */
    to: string;
}
