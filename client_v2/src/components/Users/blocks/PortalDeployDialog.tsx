import { createEffect, createSignal } from 'solid-js';
import cn from 'clsx';

import intl from 'panel/common/intl';
import theme from 'panel/lib/theme';
import { Button } from 'panel/common/ui/Button';
import { Dialog } from 'panel/common/ui/Dialog';
import { Input } from 'panel/common/controls/Input';
import { downloadPortalPackage, saveSettings, usersState } from 'panel/stores/users';

import s from './PortalDeployDialog.module.pcss';

type Props = {
    onClose: () => void;
};

/** parseOrigins turns the textarea into a list, dropping empty lines. */
const parseOrigins = (text: string): string[] =>
    text
        .split('\n')
        .map((line) => line.trim())
        .filter(Boolean);

/**
 * PortalDeployDialog configures where the portal front-end is deployed and
 * hands out the package for it.  The address of the API and the origins the API
 * accepts have to agree, so both are edited here rather than in two places.
 */
export const PortalDeployDialog = (props: Props) => {
    const [apiBase, setApiBase] = createSignal('');
    const [origins, setOrigins] = createSignal('');

    createEffect(() => {
        setApiBase(usersState.settings.portal_api_base ?? '');
        setOrigins((usersState.settings.portal_origins ?? []).join('\n'));
    });

    const persist = async (): Promise<boolean> =>
        saveSettings({
            portal_api_base: apiBase().trim(),
            portal_origins: parseOrigins(origins()),
        });

    const save = async () => {
        if (await persist()) {
            props.onClose();
        }
    };

    // The package carries the saved address, so an unsaved edit would end up
    // in a package that points at the old one.
    const download = async () => {
        if (await persist()) {
            await downloadPortalPackage();
        }
    };

    return (
        <Dialog
            visible
            onClose={props.onClose}
            title={intl.getMessage('users_portal_deploy')}
            class={s.dialog}
        >
            <div class={s.body}>
                <div class={s.hint}>{intl.getMessage('users_portal_deploy_hint')}</div>

                <div class={s.field}>
                    <label for="portal-api-base" class={theme.text.t3}>
                        {intl.getMessage('users_portal_api_base')}
                    </label>
                    <Input
                        id="portal-api-base"
                        size="small"
                        class={s.input}
                        value={apiBase()}
                        placeholder="https://dns.example.com:3004"
                        onChange={(e) => setApiBase(e.currentTarget.value)}
                        data-testid="portal-api-base"
                    />
                    <div class={s.note}>{intl.getMessage('users_portal_api_base_note')}</div>
                </div>

                <div class={s.field}>
                    <label for="portal-origins" class={theme.text.t3}>
                        {intl.getMessage('users_portal_origins')}
                    </label>
                    <textarea
                        id="portal-origins"
                        class={s.textarea}
                        rows="3"
                        value={origins()}
                        placeholder="https://portal.example.com"
                        onInput={(e) => setOrigins(e.currentTarget.value)}
                        data-testid="portal-origins"
                    />
                    <div class={s.note}>{intl.getMessage('users_portal_origins_note')}</div>
                </div>

                <div class={s.note}>{intl.getMessage('users_portal_https_note')}</div>
            </div>

            <div class={cn(theme.dialog.footer, s.footer)}>
                <Button
                    size="small"
                    variant="secondary"
                    onClick={download}
                    class={theme.dialog.button}
                    data-testid="portal-download"
                >
                    {intl.getMessage('users_portal_download')}
                </Button>
                <Button
                    size="small"
                    disabled={usersState.processingSave}
                    onClick={save}
                    class={theme.dialog.button}
                    data-testid="portal-save"
                >
                    {intl.getMessage('save')}
                </Button>
            </div>
        </Dialog>
    );
};
