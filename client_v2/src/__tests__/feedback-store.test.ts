import { beforeEach, describe, expect, it, vi } from 'vitest';

const { mocks } = vi.hoisted(() => {
    const mocks = {
        portalFeedbackList: vi.fn(),
        portalFeedbackRead: vi.fn(),
        portalFeedbackDelete: vi.fn(),
        portalFeedbackUpdate: vi.fn(),
        addErrorToast: vi.fn(),
    };

    return { mocks };
});

vi.mock('panel/api/generated', () => ({
    portalFeedbackList: mocks.portalFeedbackList,
    portalFeedbackRead: mocks.portalFeedbackRead,
    portalFeedbackDelete: mocks.portalFeedbackDelete,
    portalFeedbackUpdate: mocks.portalFeedbackUpdate,
}));

vi.mock('panel/stores/toasts', () => ({
    addErrorToast: mocks.addErrorToast,
}));

import {
    feedbackState,
    feedbackVisible,
    getFeedback,
    markAllRead,
    removeFeedback,
    updateFeedback,
} from 'panel/stores/feedback';
import type { PortalFeedback } from 'panel/api/generated';

/** msg builds a message, overriding only the fields a case cares about. */
const msg = (over: Partial<PortalFeedback>): PortalFeedback => ({
    id: 'id',
    content: 'body',
    created_at: 1700000000,
    read: false,
    ...over,
});

describe('feedbackVisible', () => {
    const items = [
        msg({ id: 'a', read: false, resolved: false }),
        msg({ id: 'b', read: true, resolved: false }),
        msg({ id: 'c', read: false, resolved: true }),
        msg({ id: 'd', read: true, resolved: true }),
    ];

    it('returns everything for the all filter', () => {
        expect(feedbackVisible(items, 'all').map((f) => f.id)).toEqual(['a', 'b', 'c', 'd']);
    });

    it('returns only the unresolved for open', () => {
        expect(feedbackVisible(items, 'open').map((f) => f.id)).toEqual(['a', 'b']);
    });

    it('returns only the resolved for resolved', () => {
        expect(feedbackVisible(items, 'resolved').map((f) => f.id)).toEqual(['c', 'd']);
    });

    it('returns only the unread for unread', () => {
        expect(feedbackVisible(items, 'unread').map((f) => f.id)).toEqual(['a', 'c']);
    });

    it('treats open and unread as different sets', () => {
        const open = feedbackVisible(items, 'open').map((f) => f.id);
        const unread = feedbackVisible(items, 'unread').map((f) => f.id);

        expect(open).not.toEqual(unread);
    });

    it('does not modify the input', () => {
        const ids = items.map((f) => f.id);

        feedbackVisible(items, 'unread');

        expect(items.map((f) => f.id)).toEqual(ids);
    });
});

describe('feedback store', () => {
    beforeEach(async () => {
        vi.clearAllMocks();

        mocks.portalFeedbackList.mockResolvedValue({
            items: [
                msg({ id: 'a', read: false, resolved: false }),
                msg({ id: 'b', read: true, resolved: true }),
            ],
            unread: 1,
            open: 1,
        });
        mocks.portalFeedbackUpdate.mockResolvedValue({ item: msg({ id: 'a' }) });
        mocks.portalFeedbackRead.mockResolvedValue(undefined);
        mocks.portalFeedbackDelete.mockResolvedValue(undefined);

        await getFeedback();
    });

    it('loads the list and the counters', () => {
        expect(mocks.portalFeedbackList).toHaveBeenCalledTimes(1);
        expect(feedbackState.items.map((f) => f.id)).toEqual(['a', 'b']);
        expect(feedbackState.unread).toBe(1);
        expect(feedbackState.open).toBe(1);
        expect(feedbackState.loading).toBe(false);
    });

    it('replaces the item with the one the API returns and sends only the patch', async () => {
        const updated = msg({ id: 'a', resolved: true, read: true });
        mocks.portalFeedbackUpdate.mockResolvedValue({ item: updated });

        await updateFeedback('a', { resolved: true });

        expect(mocks.portalFeedbackUpdate).toHaveBeenCalledWith({ id: 'a', resolved: true });
        expect(feedbackState.items.find((f) => f.id === 'a')).toEqual(updated);
        expect(feedbackState.open).toBe(0);
        expect(feedbackState.processing).toBe(false);
    });

    it('sends only the fields present in the patch', async () => {
        mocks.portalFeedbackUpdate.mockResolvedValue({ item: msg({ id: 'a', reply: 'reply text' }) });

        await updateFeedback('a', { reply: 'reply text' });

        expect(mocks.portalFeedbackUpdate).toHaveBeenCalledWith({ id: 'a', reply: 'reply text' });
    });

    it('shows an error toast and clears processing when the update fails', async () => {
        mocks.portalFeedbackUpdate.mockRejectedValue(new Error('boom'));

        await updateFeedback('a', { resolved: true });

        expect(mocks.addErrorToast).toHaveBeenCalled();
        expect(feedbackState.processing).toBe(false);
    });

    it('marks every message read', async () => {
        await markAllRead();

        expect(mocks.portalFeedbackRead).toHaveBeenCalledTimes(1);
        expect(feedbackState.unread).toBe(0);
        expect(feedbackState.items.every((f) => f.read)).toBe(true);
    });

    it('removes a deleted message from the state', async () => {
        await removeFeedback('a');

        expect(mocks.portalFeedbackDelete).toHaveBeenCalledWith({ id: 'a' });
        expect(feedbackState.items.map((f) => f.id)).toEqual(['b']);
    });

    it('keeps the server counters when the list is capped', async () => {
        // 后端只回最近 200 条，计数却是全量的，所以徽标只能按增量走，
        // 不能拿手里这几条重新数。
        mocks.portalFeedbackList.mockResolvedValue({
            items: [msg({ id: 'a', read: false, resolved: false })],
            unread: 7,
            open: 5,
        });
        await getFeedback();
        expect(feedbackState.unread).toBe(7);
        expect(feedbackState.open).toBe(5);

        mocks.portalFeedbackUpdate.mockResolvedValue({
            item: msg({ id: 'a', read: true, resolved: true }),
        });
        await updateFeedback('a', { resolved: true });

        expect(feedbackState.unread).toBe(6);
        expect(feedbackState.open).toBe(4);
    });

    it('drops the counters of a deleted message', async () => {
        mocks.portalFeedbackList.mockResolvedValue({
            items: [msg({ id: 'a', read: false, resolved: false })],
            unread: 3,
            open: 2,
        });
        await getFeedback();

        await removeFeedback('a');

        expect(feedbackState.unread).toBe(2);
        expect(feedbackState.open).toBe(1);
    });
});
