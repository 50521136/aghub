import { createMemo, createSignal, For, onMount, Show } from 'solid-js';
import { createStore } from 'solid-js/store';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { ConfirmDialog } from 'panel/common/ui/ConfirmDialog';
import { Icon } from 'panel/common/ui/Icon';
import { Table, type TableColumn } from 'panel/common/ui/Table';
import { Tooltip } from 'panel/common/ui/Tooltip';
import type { PortalFeedback } from 'panel/api/generated';
import {
    feedbackState,
    feedbackVisible,
    getFeedback,
    markAllRead,
    removeFeedback,
    updateFeedback,
} from 'panel/stores/feedback';
import type { FeedbackFilter } from 'panel/stores/feedback';

import { FeedbackDialog } from './blocks/FeedbackDialog';
import s from './Feedback.module.pcss';

/** The filters of the toolbar, in display order. */
const FILTERS: FeedbackFilter[] = ['all', 'open', 'resolved', 'unread'];

/** filterLabel returns the localized label of a filter. */
const filterLabel = (filter: FeedbackFilter): string => {
    switch (filter) {
        case 'open':
            return intl.getMessage('feedback_filter_open');
        case 'resolved':
            return intl.getMessage('feedback_filter_resolved');
        case 'unread':
            return intl.getMessage('feedback_filter_unread');
        default:
            return intl.getMessage('feedback_filter_all');
    }
};

/** pad left-pads a number to two digits. */
const pad = (n: number): string => String(n).padStart(2, '0');

/**
 * formatTime renders a Unix timestamp in seconds as a compact local date and
 * time.  A locale-formatted string is wider than the column, and the list wants
 * one line per row.
 */
const formatTime = (seconds: number): string => {
    if (!seconds) {
        return '';
    }

    const d = new Date(seconds * 1000);

    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
        d.getHours(),
    )}:${pad(d.getMinutes())}`;
};

export const Feedback = () => {
    const [filter, setFilter] = createSignal<FeedbackFilter>('all');
    // The reply box is editable before it is saved, so its text lives here,
    // keyed by message id, and falls back to the saved reply.
    const [drafts, setDrafts] = createStore<Record<string, string>>({});
    const [confirmDelete, setConfirmDelete] = createSignal('');
    // The detail dialog holds an id rather than the message itself, so that it
    // keeps showing the version the store holds after every write.
    const [selectedId, setSelectedId] = createSignal('');

    onMount(() => {
        void getFeedback();
    });

    const items = createMemo(() => feedbackVisible(feedbackState.items, filter()));

    const resolvedCount = createMemo(() => feedbackState.items.filter((f) => f.resolved).length);

    /**
     * filterCount is the number a filter chip shows.  The server caps the list
     * it returns, so the read and open totals are taken from the counters it
     * sends; the rest are counted over the messages held here.
     */
    const filterCount = (f: FeedbackFilter): number => {
        switch (f) {
            case 'open':
                return feedbackState.open;
            case 'resolved':
                return resolvedCount();
            case 'unread':
                return feedbackState.unread;
            default:
                return feedbackState.items.length;
        }
    };

    const selectedItem = createMemo(() => feedbackState.items.find((f) => f.id === selectedId()));

    /** replyDraft is the text in a message's reply box. */
    const replyDraft = (f: PortalFeedback) => drafts[f.id] ?? f.reply ?? '';

    const doDelete = () => {
        const id = confirmDelete();

        if (!id) {
            return;
        }

        void removeFeedback(id);
        setConfirmDelete('');
        setSelectedId('');
    };

    /** renderBadges is the status column: the read, resolution and visibility. */
    const renderBadges = (f: PortalFeedback) => (
        <div class={s.badges}>
            <Show when={!f.read}>
                <span class={cn(s.badge, s.badgeUnread)}>
                    {intl.getMessage('feedback_status_unread')}
                </span>
            </Show>
            <Show when={f.resolved}>
                <span class={cn(s.badge, s.badgeResolved)}>
                    {intl.getMessage('feedback_status_resolved')}
                </span>
            </Show>
            <span class={cn(s.badge, { [s.badgePrivate]: f.private })}>
                {intl.getMessage(f.private ? 'feedback_status_private' : 'feedback_status_public')}
            </span>
        </div>
    );

    const columns = createMemo<TableColumn<PortalFeedback>[]>(() => [
        {
            key: 'status',
            width: 150,
            sortable: false,
            header: { text: intl.getMessage('feedback_col_status') },
            render: (_value, f) => (
                <div class={theme.table.cell}>
                    <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                        {intl.getMessage('feedback_col_status')}
                    </div>
                    <div class={theme.table.cellValue}>{renderBadges(f)}</div>
                </div>
            ),
        },
        {
            key: 'user',
            minWidth: 130,
            maxWidth: 200,
            sortable: false,
            header: { text: intl.getMessage('feedback_col_user') },
            render: (_value, f) => (
                <div class={theme.table.cell}>
                    <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                        {intl.getMessage('feedback_col_user')}
                    </div>
                    <div class={theme.table.cellValue}>
                        <div class={s.userBox}>
                            <span class={s.userName}>
                                {f.name || f.uid || intl.getMessage('feedback_anonymous')}
                            </span>
                            <Show when={f.uid && f.name}>
                                <span class={s.userUid}>{f.uid}</span>
                            </Show>
                        </div>
                    </div>
                </div>
            ),
        },
        {
            key: 'content',
            sortable: false,
            header: { text: intl.getMessage('feedback_col_content') },
            render: (_value, f) => (
                <div class={theme.table.cell} data-testid={`feedback-item-${f.id}`}>
                    <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                        {intl.getMessage('feedback_col_content')}
                    </div>
                    <div class={theme.table.cellValue}>
                        <div class={theme.table.cellValueText} title={f.content}>
                            {f.content}
                        </div>
                    </div>
                </div>
            ),
        },
        {
            key: 'time',
            width: 150,
            sortable: false,
            header: { text: intl.getMessage('feedback_col_time') },
            render: (_value, f) => (
                <div class={theme.table.cell}>
                    <div class={cn(theme.table.cellLabel, s.cellLabel)}>
                        {intl.getMessage('feedback_col_time')}
                    </div>
                    <div class={theme.table.cellValue}>
                        <div class={theme.table.cellValueText}>{formatTime(f.created_at)}</div>
                    </div>
                </div>
            ),
        },
        {
            key: 'actions',
            width: 108,
            sortable: false,
            class: s.actionsCellOuter,
            header: {
                text: intl.getMessage('feedback_col_actions'),
                // The action buttons are right-aligned in their cell, so the
                // header label has to be right-aligned as well.
                render: () => (
                    <span class={s.actionsHeader}>
                        {intl.getMessage('feedback_col_actions')}
                    </span>
                ),
            },
            render: (_value, f) => (
                <div
                    class={cn(theme.table.cellActions, s.actions)}
                    onClick={(e) => e.stopPropagation()}
                >
                    <Tooltip
                        content={intl.getMessage(
                            f.resolved ? 'feedback_reopen' : 'feedback_resolve',
                        )}
                    >
                        <button
                            type="button"
                            class={theme.table.action}
                            disabled={feedbackState.processing}
                            onClick={() =>
                                void updateFeedback(f.id, { resolved: !f.resolved })
                            }
                            data-testid={`feedback-resolve-${f.id}`}
                        >
                            <Icon icon={f.resolved ? 'refresh' : 'check'} />
                            <span class={theme.table.actionLabel}>
                                {intl.getMessage(
                                    f.resolved ? 'feedback_reopen' : 'feedback_resolve',
                                )}
                            </span>
                        </button>
                    </Tooltip>

                    <Tooltip
                        content={intl.getMessage(
                            f.private ? 'feedback_make_public' : 'feedback_make_private',
                        )}
                    >
                        <button
                            type="button"
                            class={theme.table.action}
                            disabled={feedbackState.processing}
                            onClick={() => void updateFeedback(f.id, { private: !f.private })}
                            data-testid={`feedback-visibility-${f.id}`}
                        >
                            <Icon icon={f.private ? 'eye_open' : 'eye_close'} />
                            <span class={theme.table.actionLabel}>
                                {intl.getMessage(
                                    f.private ? 'feedback_make_public' : 'feedback_make_private',
                                )}
                            </span>
                        </button>
                    </Tooltip>

                    <Tooltip content={intl.getMessage('feedback_delete')}>
                        <button
                            type="button"
                            class={cn(theme.table.action, theme.table.action_danger)}
                            disabled={feedbackState.processing}
                            onClick={() => setConfirmDelete(f.id)}
                            data-testid={`feedback-delete-${f.id}`}
                        >
                            <Icon icon="delete" />
                            <span class={theme.table.actionLabel}>
                                {intl.getMessage('feedback_delete')}
                            </span>
                        </button>
                    </Tooltip>
                </div>
            ),
        },
    ]);

    return (
        <div class={theme.layout.container}>
            <div class={theme.layout.containerIn}>
                <h1
                    class={cn(theme.layout.title, theme.title.h4, theme.title.h3_tablet)}
                    data-testid="feedback-title"
                >
                    {intl.getMessage('feedback_title')}
                </h1>

                <div class={s.body}>
                    <p class={cn(theme.text.t3, s.desc)}>{intl.getMessage('feedback_hint')}</p>

                    <div class={s.toolbar}>
                        <div class={s.chips}>
                            <For each={FILTERS}>
                                {(f) => (
                                    <button
                                        type="button"
                                        class={cn(s.chip, { [s.chipActive]: filter() === f })}
                                        onClick={() => setFilter(f)}
                                        data-testid={`feedback-filter-${f}`}
                                    >
                                        {filterLabel(f)}
                                        <span class={s.chipCount}>{filterCount(f)}</span>
                                    </button>
                                )}
                            </For>
                        </div>

                        <div class={s.toolbarActions}>
                            <button
                                type="button"
                                class={s.toolButton}
                                disabled={feedbackState.unread === 0 || feedbackState.processing}
                                onClick={() => void markAllRead()}
                                data-testid="feedback-mark-read"
                            >
                                {intl.getMessage('feedback_mark_read')}
                            </button>
                            <button
                                type="button"
                                class={s.toolButton}
                                disabled={feedbackState.loading}
                                onClick={() => void getFeedback()}
                                data-testid="feedback-reload"
                            >
                                {intl.getMessage('feedback_reload')}
                            </button>
                        </div>
                    </div>

                    <div class={s.tableSection}>
                        <Table
                            data={items()}
                            columns={columns()}
                            loading={feedbackState.loading}
                            pagination={false}
                            sortable={false}
                            getRowId={(f) => f.id}
                            onRowClick={(f) => setSelectedId(f.id)}
                            class={s.tableRows}
                            tableRowClass={s.cardRow}
                            emptyTable={
                                <div class={s.empty}>
                                    <Icon icon="faq" class={s.emptyIcon} />
                                    <span class={theme.text.t2}>
                                        {intl.getMessage('feedback_empty')}
                                    </span>
                                </div>
                            }
                        />
                    </div>
                </div>
            </div>

            <Show when={selectedItem()}>
                {(f) => (
                    <FeedbackDialog
                        item={f()}
                        draft={replyDraft(f())}
                        processing={feedbackState.processing}
                        onDraft={(value) => setDrafts(f().id, value)}
                        onSaveReply={() => void updateFeedback(f().id, { reply: replyDraft(f()) })}
                        onResolve={() =>
                            void updateFeedback(f().id, { resolved: !f().resolved })
                        }
                        onToggleVisibility={() =>
                            void updateFeedback(f().id, { private: !f().private })
                        }
                        onDelete={() => setConfirmDelete(f().id)}
                        onClose={() => setSelectedId('')}
                    />
                )}
            </Show>

            <Show when={confirmDelete()}>
                <ConfirmDialog
                    onClose={() => setConfirmDelete('')}
                    onConfirm={doDelete}
                    submitDisabled={feedbackState.processing}
                    buttonText={intl.getMessage('yes_remove')}
                    cancelText={intl.getMessage('cancel')}
                    buttonVariant="danger"
                    title={intl.getMessage('feedback_delete_title')}
                    text={intl.getMessage('feedback_delete_desc')}
                    submitTestId="feedback-delete-confirm"
                />
            </Show>
        </div>
    );
};
