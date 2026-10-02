/**
 * Bulk user creation request
 */
export interface UserBulkAddRequest {
    /** One entry per line: identifier[,name[,limit[,period[,expire days]]]].  Blank lines and lines starting with # are ignored. */
    text: string;
    enabled?: boolean;
}
