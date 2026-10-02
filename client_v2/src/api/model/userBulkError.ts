/**
 * A problem with one line of a bulk payload
 */
export interface UserBulkError {
    line?: number;
    text?: string;
    message?: string;
}
