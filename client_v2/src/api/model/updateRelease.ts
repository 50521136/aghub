import type { UpdateAsset } from './updateAsset';

/**
 * A GitHub release.
 */
export interface UpdateRelease {
    version?: string;
    tag_name?: string;
    name?: string;
    notes?: string;
    url?: string;
    published_at?: string;
    assets?: UpdateAsset[];
}
