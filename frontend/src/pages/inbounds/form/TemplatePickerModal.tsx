import { useTranslation } from 'react-i18next';
import { List, Modal, Typography } from 'antd';

import type { InboundTemplate } from '@/lib/xray/inbound-templates';

interface TemplatePickerModalProps {
  open: boolean;
  templates: InboundTemplate[];
  onPick: (template: InboundTemplate) => void;
  onClose: () => void;
}

export default function TemplatePickerModal({ open, templates, onPick, onClose }: TemplatePickerModalProps) {
  const { t } = useTranslation();

  return (
    <Modal
      open={open}
      title={t('pages.inbounds.form.templatePickerTitle')}
      okText={null}
      cancelText={t('close')}
      onCancel={onClose}
      width={560}
      destroyOnHidden
    >
      <Typography.Paragraph type="secondary">
        {t('pages.inbounds.form.templatePickerSubtitle')}
      </Typography.Paragraph>
      <List
        dataSource={templates}
        renderItem={(template) => (
          <List.Item
            className="inbound-template-row"
            onClick={() => onPick(template)}
            style={{ cursor: 'pointer', paddingInline: 8 }}
          >
            <List.Item.Meta
              title={<Typography.Text strong>{template.title}</Typography.Text>}
              description={template.description}
            />
          </List.Item>
        )}
      />
    </Modal>
  );
}