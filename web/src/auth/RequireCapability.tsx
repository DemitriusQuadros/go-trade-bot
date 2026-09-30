import React from 'react';
import { Link } from 'react-router-dom';
import { Lock } from 'lucide-react';
import { Capability } from '@/api/types';
import { useAuth } from '@/context/AuthContext';
import { Card } from '@/components/ui/Card';

const STRINGS = {
  title: "You don't have access to this page",
  body: 'Your account is missing the permission this page needs. Ask an admin if you think you should have it.',
  back: 'Back to the dashboard',
};

// Route-level guard for pages a capability unlocks (Settings, Users, new
// strategy/agent). Hiding the nav entry isn't enough - a typed URL or an old
// bookmark still lands here. The backend enforces the same rule.
export function RequireCapability({ cap, children }: { cap: Capability; children: React.ReactNode }) {
  const { can } = useAuth();
  if (can(cap)) return <>{children}</>;
  return (
    <div className="container-custom max-w-xl mx-auto mt-12">
      <Card className="p-6 text-center space-y-3" data-testid="no-access">
        <Lock className="w-8 h-8 mx-auto text-muted-foreground" />
        <h1 className="text-lg font-semibold text-foreground">{STRINGS.title}</h1>
        <p className="text-sm text-muted-foreground">{STRINGS.body}</p>
        <Link to="/" className="inline-block text-sm text-primary hover:underline">
          {STRINGS.back}
        </Link>
      </Card>
    </div>
  );
}
