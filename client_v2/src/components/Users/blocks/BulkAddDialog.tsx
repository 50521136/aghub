import { For, Show, createMemo, createSignal } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Dialog } from 'panel/common/ui/Dialog';
import { Switch } from 'panel/common/controls/Switch';
import type { UserBulkError } from 'panel/api/model';
import { bulkAddUsers } from 'panel/stores/users';

import s from './BulkAddDialog.module.pcss';

type Props = {
    onClose: () => void;
};

/** countEntries returns the number of lines that will create a user. */
const countEntries = (text: string): number =>
    text
        .split('\n')
        .map((line) => line.trim())
        .filter((line) => line !== '' && !line.startsWith('#')).length;

export const BulkAddDialog = (props: Props) => {
    const [text, setText] = createSignal('');
    const [enabled, setEnabled] = createSignal(true);
    const [errors, setErrors] = createSignal<UserBulkError[]>([]);
    const [submitting, setSubmitting] = createSignal(false);

    const entries = createMemo(() => countEntries(text()));

    const submit = async () => {
        setSubmitting(true);
        setErrors([]);

        const res = await bulkAddUsers(text(), enabled());

        setSubmitting(false);

        if (!res) {
            return;
        }

        if (res.errors && res.errors.length > 0) {
            setErrors(res.errors);

            return;
        }

        props.onClose();
    };

    return (
        <Dialog
            visible
            onClose={props.onClose}
            title={intl.getMessage('users_bulk_title')}
            class={s.dialog}
        >
            <div class={s.form}>
                <div class={s.textField}>
                    <label class={s.fieldLabel} for="users-bulk-text">
                        {intl.getMessage('users_bulk_field')}
                    </label>
                    <textarea
                        id="users-bulk-text"
                        class={s.textarea}
                        rows={9}
                        spellcheck={false}
                        placeholder={intl.getMessage('users_bulk_placeholder')}
                        value={text()}
                        onInput={(e) => {
                            setText(e.currentTarget.value);
                            setErrors([]);
                        }}
                        data-testid="users-bulk-text"
                    />
                    <div class={s.hint}>{intl.getMessage('users_bulk_hint')}</div>
                </div>

                <div class={s.counter}>
                    {intl.getMessage('users_bulk_count', { count: String(entries()) })}
                </div>

                <Switch
                    id="users-bulk-enabled"
                    checked={enabled()}
                    onChange={() => setEnabled((prev) => !prev)}
                >
                    {intl.getMessage('users_bulk_enabled')}
                </Switch>

                <Show when={errors().length > 0}>
                    <div class={s.errorBox} data-testid="users-bulk-errors">
                        <div class={s.errorTitle}>
                            {intl.getMessage('users_bulk_errors_title', {
                                count: String(errors().length),
                            })}
                        </div>
                        <ul class={s.errorList}>
                            <For each={errors()}>
                                {(err) => (
                                    <li class={s.errorItem}>
                                        <Show when={err.line > 0}>
                                            <span class={s.errorLine}>
                                                {intl.getMessage('users_bulk_error_line', {
                                                    line: String(err.line),
                                                })}
                                            </span>
                                        </Show>
                                        <span class={s.errorMessage}>{err.message}</span>
                                        <Show when={err.text}>
                                            <code class={s.errorText}>{err.text}</code>
                                        </Show>
                                    </li>
                                )}
                            </For>
                        </ul>
                    </div>
                </Show>
            </div>

            {/* A sibling of the padded form, matching UserDialog, so the buttons
                keep the same 16px inset as the title. */}
            <div class={cn(theme.dialog.footer, s.footer)}>
                <Button
                    size="small"
                    variant="secondary"
                    onClick={props.onClose}
                    class={theme.dialog.button}
                >
                    {intl.getMessage('cancel')}
                </Button>
                <Button
                    size="small"
                    onClick={submit}
                    class={theme.dialog.button}
                    disabled={submitting() || entries() === 0}
                    data-testid="users-bulk-submit"
                >
                    {intl.getMessage('users_bulk_submit')}
                </Button>
            </div>
        </Dialog>
    );
};
